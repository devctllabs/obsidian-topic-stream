"""Caller-visible local profile selection and ownership guarantees."""

import contextlib
import io
import json
import tempfile
import unittest
import urllib.request
from pathlib import Path

from tools import performance


class FakeRuntime:
    def __init__(
        self, *, failed_service=None, go_status=0, ready=True, probe_smoke=False
    ):
        self.failed_service = failed_service
        self.go_status = go_status
        self.ready = ready
        self.calls = []
        self.labels = {}
        self.removed = []
        self.test_env = None
        self.probe_smoke = probe_smoke
        self.smoke_result = None

    def run(self, argv, *, env=None, output=None, timeout=120):
        args = tuple(argv)
        self.calls.append(args)
        if args[:2] == ("docker", "run"):
            name = args[args.index("--name") + 1]
            label = args[args.index("--label") + 1].split("=", 1)[1]
            self.labels[name] = label
            if self.failed_service and self.failed_service in name:
                return performance.CommandResult(125)
            return performance.CommandResult(0, name)
        if args[:2] == ("docker", "port"):
            port = {"5432/tcp": 55432, "6333/tcp": 56333, "6334/tcp": 56334}[args[3]]
            return performance.CommandResult(0, f"127.0.0.1:{port}\n")
        if args[:2] == ("docker", "inspect"):
            return performance.CommandResult(
                0, self.labels.get(args[-1], "unrelated-owner")
            )
        if args[:3] == ("docker", "rm", "--force"):
            self.removed.append(args[-1])
            self.labels.pop(args[-1], None)
        if args[:2] == ("go", "test"):
            self.test_env = dict(env)
            if self.probe_smoke and env.get("TOPIC_STREAM_PILOT_OLLAMA"):
                request = urllib.request.Request(
                    env["TOPIC_STREAM_PILOT_OLLAMA"] + "/v1/embeddings",
                    data=json.dumps(
                        {"input": ["first", "second"], "dimensions": 3}
                    ).encode(),
                    headers={"Content-Type": "application/json"},
                )
                with urllib.request.urlopen(request) as response:
                    self.smoke_result = json.load(response)
            if output is not None:
                output.write_text('{"Action":"pass"}\n')
            return performance.CommandResult(self.go_status, elapsed_seconds=0.5)
        return performance.CommandResult(0)

    def wait_ready(self, resource, name, endpoint, timeout):
        return self.ready


class PerformanceCLI(unittest.TestCase):
    def test_postgres_readiness_waits_for_tcp_after_the_temporary_init_server(self):
        class InitializingPostgres(performance.SubprocessRuntime):
            def __init__(self):
                super().__init__(Path.cwd())
                self.probes = 0

            def run(self, argv, **options):
                self.probes += 1
                # The image's temporary init server accepts Unix sockets, never TCP.
                tcp = "-h" in argv and argv[argv.index("-h") + 1] == "127.0.0.1"
                return performance.CommandResult(1 if tcp and self.probes == 1 else 0)

        runtime = InitializingPostgres()
        self.assertTrue(
            runtime.wait_ready(performance.RESOURCES["postgres"], "owned", "", 2)
        )
        self.assertEqual(
            2, runtime.probes, "Unix-only init readiness must not finish setup"
        )

    def test_default_synthetic_plan_is_100k_with_fixed_resource_envelope(self):
        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            code = performance.main(["qdrant-search", "--dry-run"], env={})
        self.assertEqual(0, code)
        plan = json.loads(output.getvalue())
        self.assertEqual(100000, plan["points"])
        self.assertEqual("integration,performance", plan["profile"]["tags"])
        self.assertEqual(1, len(plan["resources"]))
        self.assertEqual("qdrant", plan["resources"][0]["name"])
        self.assertEqual(2, plan["resources"][0]["cpus"])
        self.assertEqual(6, plan["resources"][0]["memory_gib"])
        self.assertEqual(1024, plan["dimensions"])

    def test_zero_dimensions_are_rejected_instead_of_silently_using_a_default(self):
        error = io.StringIO()
        with contextlib.redirect_stderr(error):
            code = performance.main(
                ["live-index", "--smoke", "--dimensions", "0", "--dry-run"], env={}
            )
        self.assertEqual(2, code)
        self.assertIn("dimensions", error.getvalue())

    def test_ensure_profile_does_not_claim_to_load_a_synthetic_corpus(self):
        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            code = performance.main(["qdrant-ensure", "--dry-run"], env={})
        self.assertEqual(0, code)
        plan = json.loads(output.getvalue())
        self.assertIsNone(plan["points"])
        self.assertIsNone(plan["dimensions"])

    def test_live_requires_inputs_and_never_leaks_endpoint_credentials(self):
        for arguments in (
            ["live-index", "--dry-run"],
            [
                "live-summary",
                "--endpoint",
                "https://user:private-token@example.com/v1",
                "--model",
                "model",
                "--dry-run",
            ],
        ):
            with (
                self.subTest(arguments=arguments),
                contextlib.redirect_stderr(io.StringIO()) as error,
            ):
                self.assertEqual(2, performance.main(arguments, env={}))
                self.assertNotIn("private-token", error.getvalue())

    def test_live_index_is_bounded_and_requires_separate_build_selection(self):
        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            code = performance.main(
                [
                    "live-index",
                    "--endpoint",
                    "http://127.0.0.1:11434",
                    "--model",
                    "installed",
                    "--dimensions",
                    "768",
                    "--dry-run",
                ],
                env={},
            )
        self.assertEqual(0, code)
        plan = json.loads(output.getvalue())
        self.assertEqual(500, plan["items"])
        self.assertEqual(768, plan["dimensions"])
        self.assertEqual("integration,live", plan["profile"]["tags"])

    def test_unrelated_live_environment_cannot_change_a_synthetic_profile(self):
        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            code = performance.main(
                ["qdrant-search", "--points", "1000000", "--dry-run"],
                env={
                    "TOPIC_STREAM_PILOT_OLLAMA": "http://real-model.example",
                    "TOPIC_STREAM_PILOT_MODEL": "real-model",
                },
            )
        self.assertEqual(0, code)
        plan = json.loads(output.getvalue())
        self.assertEqual(1000000, plan["points"])
        self.assertIsNone(plan["endpoint"])
        self.assertIsNone(plan["model"])

    def test_execution_owns_resources_replaces_external_urls_and_cleans_up(self):
        runtime = FakeRuntime()
        with (
            tempfile.TemporaryDirectory() as directory,
            contextlib.redirect_stdout(io.StringIO()) as output,
        ):
            result = performance.main(
                ["sql-feed-plan", "--output", directory],
                env={
                    "TOPIC_STREAM_TEST_POSTGRES_DSN": "postgres://private:secret@production/db",
                    "TOPIC_STREAM_TEST_QDRANT_URL": "http://production:6334",
                },
                runtime=runtime,
            )
            self.assertEqual(0, result)
            self.assertIn(
                "127.0.0.1:55432", runtime.test_env["TOPIC_STREAM_TEST_POSTGRES_DSN"]
            )
            self.assertNotIn("TOPIC_STREAM_TEST_QDRANT_URL", runtime.test_env)
            self.assertNotIn("GOWORK", runtime.test_env)
            self.assertNotIn("secret", output.getvalue())
            self.assertEqual(1, len(runtime.removed))
            removal = next(
                call
                for call in runtime.calls
                if call[:3] == ("docker", "rm", "--force")
            )
            self.assertIn(
                "--volumes", removal, "owned anonymous database volumes must not leak"
            )
            self.assertFalse(runtime.labels)
            record = json.loads((Path(directory) / "run.json").read_text())
            self.assertEqual("complete", record["status"])
            self.assertTrue((Path(directory) / "go-test.jsonl").exists())

    def test_partial_second_service_failure_removes_both_owned_resources(self):
        runtime = FakeRuntime(failed_service="qdrant")
        with (
            tempfile.TemporaryDirectory() as directory,
            contextlib.redirect_stderr(io.StringIO()),
            contextlib.redirect_stdout(io.StringIO()),
        ):
            result = performance.main(
                ["sql-http", "--output", directory], env={}, runtime=runtime
            )
            self.assertEqual(1, result)
            self.assertEqual(2, len(runtime.removed))
            self.assertFalse(runtime.labels)
            self.assertIsNone(runtime.test_env)

    def test_test_failure_still_cleans_up_and_records_a_failed_run(self):
        runtime = FakeRuntime(go_status=1)
        with (
            tempfile.TemporaryDirectory() as directory,
            contextlib.redirect_stderr(io.StringIO()),
            contextlib.redirect_stdout(io.StringIO()),
        ):
            result = performance.main(
                ["sql-feed-plan", "--output", directory], env={}, runtime=runtime
            )
            self.assertEqual(1, result)
            self.assertEqual(1, len(runtime.removed))
            self.assertEqual(
                "failed",
                json.loads((Path(directory) / "run.json").read_text())["status"],
            )

    def test_explicit_live_smoke_uses_a_local_stub_without_a_model_endpoint(self):
        runtime = FakeRuntime(probe_smoke=True)
        with (
            tempfile.TemporaryDirectory() as directory,
            contextlib.redirect_stdout(io.StringIO()),
        ):
            result = performance.main(
                ["live-index", "--smoke", "--dimensions", "3", "--output", directory],
                env={},
                runtime=runtime,
            )
            self.assertEqual(0, result)
            self.assertEqual(
                "synthetic-smoke", runtime.test_env["TOPIC_STREAM_PILOT_MODEL"]
            )
            self.assertEqual("3", runtime.test_env["TOPIC_STREAM_PILOT_ITEMS"])
            self.assertIsNotNone(runtime.smoke_result)
            self.assertEqual(2, len(runtime.smoke_result["data"]))
            self.assertEqual(
                [1.0, 0.0, 0.0], runtime.smoke_result["data"][0]["embedding"]
            )

    def test_readiness_failure_cleans_up_without_running_tests(self):
        runtime = FakeRuntime(ready=False)
        with (
            tempfile.TemporaryDirectory() as directory,
            contextlib.redirect_stderr(io.StringIO()),
            contextlib.redirect_stdout(io.StringIO()),
        ):
            self.assertEqual(
                1,
                performance.main(
                    ["sql-http", "--output", directory], env={}, runtime=runtime
                ),
            )
            self.assertEqual(1, len(runtime.removed))
            self.assertIsNone(runtime.test_env)

    def test_cleanup_refuses_a_resource_whose_ownership_has_changed(self):
        class ChangedOwner(FakeRuntime):
            def run(self, argv, **options):
                if tuple(argv[:2]) == ("docker", "inspect"):
                    return performance.CommandResult(0, "another-run")
                return super().run(argv, **options)

        runtime = ChangedOwner()
        with (
            tempfile.TemporaryDirectory() as directory,
            contextlib.redirect_stderr(io.StringIO()),
            contextlib.redirect_stdout(io.StringIO()),
        ):
            self.assertEqual(
                1,
                performance.main(
                    ["sql-feed-plan", "--output", directory], env={}, runtime=runtime
                ),
            )
            self.assertFalse(runtime.removed)
            self.assertEqual(1, len(runtime.labels))
            record = json.loads((Path(directory) / "run.json").read_text())
            self.assertTrue(record["cleanup_errors"])

    def test_invalid_report_path_fails_before_resource_creation(self):
        runtime = FakeRuntime()
        with (
            tempfile.TemporaryDirectory() as directory,
            contextlib.redirect_stderr(io.StringIO()),
        ):
            path = Path(directory) / "file"
            path.write_text("keep this file")
            self.assertEqual(
                1,
                performance.main(
                    ["sql-feed-plan", "--output", str(path)], env={}, runtime=runtime
                ),
            )
            self.assertFalse(runtime.calls)
            self.assertEqual("keep this file", path.read_text())


if __name__ == "__main__":
    unittest.main()
