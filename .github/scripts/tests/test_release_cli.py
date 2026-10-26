"""Offline compute and plugin-preparation CLI contracts."""

import contextlib
import io
import json
import subprocess
import tempfile
import unittest
from pathlib import Path

import compute_release_version as compute
import prepare_plugin_release as prepare
import release_policy as policy


class ReleaseCLI(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        self.git("init", "--initial-branch=main")
        for path, value in {
            "plugin/package.json": {"name": "plugin", "version": "0.1.0"},
            "plugin/manifest.json": {"version": "0.1.0", "minAppVersion": "1.8.0"},
            "plugin/versions.json": {"0.1.0": "1.8.0"},
        }.items():
            target = self.root / path
            target.parent.mkdir(exist_ok=True)
            target.write_text(json.dumps(value, indent=2) + "\n")
        (self.root / "internal").mkdir()
        (self.root / "internal/code.go").write_text("package internal\n")
        self.commit("fixture initial")
        self.sha = self.git("rev-parse", "HEAD")
        self.history = self.root / "history.json"
        self.history.write_text("[]")
        self.body = self.root / "prepare-body.md"

    def git(self, *args):
        return subprocess.run(
            [
                "git",
                "-c",
                "user.name=Release test",
                "-c",
                "user.email=release@example.test",
                "-c",
                "commit.gpgsign=false",
                "-c",
                "core.hooksPath=/dev/null",
                *args,
            ],
            cwd=self.root,
            capture_output=True,
            text=True,
            check=True,
        ).stdout.strip()

    def commit(self, message):
        self.git("add", "plugin", "internal")
        self.git("commit", "-m", message)

    def invoke_compute(self, *args):
        output, error = io.StringIO(), io.StringIO()
        with contextlib.redirect_stdout(output), contextlib.redirect_stderr(error):
            code = compute.main(args, root=self.root)
        return code, output.getvalue(), error.getvalue()

    def prepare_args(self, version="0.1.0", baseline="", apply=False):
        result = [
            "--sha",
            self.sha,
            "--version",
            version,
            "--baseline",
            baseline,
            "--body-output",
            str(self.body),
        ]
        return result + (["--apply"] if apply else [])

    def invoke_prepare(self, *args):
        output, error = io.StringIO(), io.StringIO()
        with contextlib.redirect_stdout(output), contextlib.redirect_stderr(error):
            code = prepare.main(args, root=self.root)
        return code, output.getvalue(), error.getvalue()

    def test_pipeline_calculates_once_and_preparation_receives_its_version(self):
        code, output, error = self.invoke_compute(
            "--component",
            "plugin",
            "--bump",
            "patch",
            "--sha",
            self.sha,
            "--history",
            str(self.history),
        )
        self.assertEqual(0, code, error)
        version = json.loads(output)["version"]
        self.assertEqual("0.1.0", version)
        before = self.git("status", "--porcelain", "--untracked-files=no")
        code, output, error = self.invoke_prepare(*self.prepare_args(version))
        self.assertEqual(0, code, error)
        self.assertEqual([], json.loads(output)["changes"])
        self.assertEqual(
            before, self.git("status", "--porcelain", "--untracked-files=no")
        )

    def test_paginated_snapshot_is_complete_and_malformed_or_missing_history_fails(
        self,
    ):
        row = {"tag_name": "0.1.0", "draft": False, "prerelease": False}
        self.assertEqual(101, len(policy.history([[row] * 100, [row]])))
        self.assertEqual((), policy.history([[]]))
        for bad in (None, {}, [{"tag_name": "0.1.0"}], [row, []]):
            with self.subTest(value=bad), self.assertRaises((TypeError, ValueError)):
                policy.history(bad)
        self.history.write_text("null")
        code, output, _ = self.invoke_compute(
            "--component",
            "plugin",
            "--bump",
            "patch",
            "--sha",
            self.sha,
            "--history",
            str(self.history),
        )
        self.assertEqual(1, code)
        self.assertFalse(output)

    def test_unpublished_existing_tag_for_other_source_is_a_conflict(self):
        self.git("tag", "0.1.0")
        (self.root / "plugin/code.ts").write_text("export {};\n")
        self.commit("fixture change")
        self.sha = self.git("rev-parse", "HEAD")
        code, _, error = self.invoke_compute(
            "--component",
            "plugin",
            "--bump",
            "patch",
            "--sha",
            self.sha,
            "--history",
            str(self.history),
        )
        self.assertEqual(1, code)
        self.assertIn("another source", error)

    def test_preparation_emits_minimal_plan_and_body(self):
        self.git("tag", "0.1.0")
        (self.root / "plugin/code.ts").write_text("export {};\n")
        self.commit("fixture change")
        self.sha = self.git("rev-parse", "HEAD")
        code, output, error = self.invoke_prepare(*self.prepare_args("0.1.1", "0.1.0"))
        self.assertEqual(0, code, error)
        plan = json.loads(output)
        self.assertEqual(3, len(plan["changes"]))
        self.assertEqual("release/plugin-0.1.1", plan["branch"])
        self.assertNotIn("existing_head", plan)
        self.assertIn("Prepare plugin 0.1.1", self.body.read_text())

    def test_dirty_worktree_is_preserved_and_not_overwritten_by_apply(self):
        self.git("tag", "0.1.0")
        (self.root / "plugin/code.ts").write_text("export {};\n")
        self.commit("fixture change")
        self.sha = self.git("rev-parse", "HEAD")
        target = self.root / "plugin/package.json"
        target.write_text('{"version":"0.1.0","privateEdit":true}')
        code, _, error = self.invoke_prepare(*self.prepare_args("0.1.1", "0.1.0", apply=True))
        self.assertEqual(1, code)
        self.assertIn("no edits applied", error)
        self.assertTrue(json.loads(target.read_text())["privateEdit"])

    def test_dry_run_reports_reviewable_edits_and_preserves_source_files(self):
        self.git("tag", "0.1.0")
        (self.root / "plugin/code.ts").write_text("export {};\n")
        self.commit("fixture change")
        self.sha = self.git("rev-parse", "HEAD")
        code, output, error = self.invoke_prepare(*self.prepare_args("0.1.1", "0.1.0"))
        self.assertEqual(0, code, error)
        plan = json.loads(output)
        self.assertEqual(3, len(plan["changes"]))
        self.assertIn("0.1.1", plan["title"])
        self.assertIn(self.sha, plan["body"])
        self.assertFalse(self.git("status", "--porcelain", "--untracked-files=no"))

    def test_apply_writes_planned_metadata(self):
        self.git("tag", "0.1.0")
        (self.root / "plugin/code.ts").write_text("export {};\n")
        self.commit("fixture change")
        self.sha = self.git("rev-parse", "HEAD")
        code, _, error = self.invoke_prepare(
            *self.prepare_args("0.1.1", "0.1.0", apply=True)
        )
        self.assertEqual(0, code, error)
        self.assertEqual(
            "0.1.1",
            json.loads((self.root / "plugin/package.json").read_text())["version"],
        )
        self.assertEqual(
            "0.1.1",
            json.loads((self.root / "plugin/manifest.json").read_text())["version"],
        )


if __name__ == "__main__":
    unittest.main()
