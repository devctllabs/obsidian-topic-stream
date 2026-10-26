#!/usr/bin/env python3
"""Plan and optionally apply reviewable plugin release metadata changes."""

from __future__ import annotations

import argparse
import dataclasses
import json
import re
import subprocess
import sys
from collections.abc import Mapping, Sequence
from dataclasses import dataclass
from pathlib import Path

from release_policy import Candidate, verify_tag, version


def git(root: Path, *arguments: str) -> str:
    """Read repository metadata without a shell or publication credentials."""
    return subprocess.run(
        ["git", *arguments],
        cwd=root,
        check=True,
        capture_output=True,
        text=True,
        timeout=30,
    ).stdout


FILES = ("plugin/package.json", "plugin/manifest.json", "plugin/versions.json")


@dataclass(frozen=True, slots=True)
class FileChange:
    """One candidate metadata edit; source controls and other files remain outside this value."""

    path: str
    contents: str


def object_source(sources: Mapping[str, str], path: str) -> dict[str, object]:
    """Decode a metadata object at the file boundary without accepting missing inputs."""
    contents = sources.get(path)
    if contents is None:
        raise ValueError(f"missing release metadata: {path}")
    value: object = json.loads(contents)
    if not isinstance(value, dict):
        raise TypeError(f"release metadata must be an object: {path}")
    return value


def stable_field(value: Mapping[str, object], field: str) -> str:
    """Read a stable-version field, rejecting wrong JSON types before comparison."""
    text = value.get(field)
    if not isinstance(text, str):
        raise TypeError(f"release metadata requires string {field}")
    version(text)
    return text


def prepare_sources(
    sources: Mapping[str, str], candidate: Candidate, historical: Mapping[str, str]
) -> tuple[FileChange, ...]:
    """Synchronize candidate fields while preserving every published minimum-host mapping."""
    if candidate.component != "plugin" or candidate.tag != candidate.version:
        raise ValueError("plugin preparation requires an exact plugin candidate")
    package, manifest, versions = (object_source(sources, path) for path in FILES)
    current = stable_field(package, "version")
    if stable_field(manifest, "version") != current:
        raise ValueError("plugin package and manifest versions disagree")
    minimum = stable_field(manifest, "minAppVersion")
    if version(current) > version(candidate.version):
        raise ValueError("plugin preparation cannot downgrade source versions")
    if candidate.baseline is not None and current not in (
        candidate.baseline,
        candidate.version,
    ):
        raise ValueError(
            "plugin source version differs from its published baseline and candidate"
        )
    for published, host in historical.items():
        if versions.get(published) != host:
            raise ValueError(f"published minimum-host mapping changed: {published}")
    for declared, host in versions.items():
        version(declared)
        if not isinstance(host, str):
            raise TypeError("plugin minimum-host mapping must contain strings")
        version(host)
    planned = (
        package | {"version": candidate.version},
        manifest | {"version": candidate.version},
        versions | {candidate.version: minimum},
    )
    originals = (package, manifest, versions)
    return tuple(
        FileChange(path, json.dumps(value, indent=2) + "\n")
        for path, value, original in zip(FILES, planned, originals, strict=True)
        if value != original
    )


class GitSource:
    """Read the immutable source tree and local tags; never mutate Git refs."""

    def __init__(self, root: Path):
        self.root = root

    def verify(self, sha: str) -> None:
        if (
            not re.fullmatch(r"[0-9a-f]{40}", sha)
            or git(self.root, "rev-parse", "HEAD").strip() != sha
        ):
            raise ValueError("release requires the exact checked-out full source SHA")

    def read(self, revision: str, path: str) -> str:
        return git(self.root, "show", f"{revision}:{path}")

    def changed(self, baseline: str | None, sha: str) -> tuple[str, ...]:
        arguments = (
            ("diff", "--no-renames", "--name-only", "-z", baseline, sha, "--")
            if baseline
            else ("ls-tree", "-r", "--name-only", "-z", sha)
        )
        return tuple(path for path in git(self.root, *arguments).split("\0") if path)

    def tag(self, name: str) -> str | None:
        return self._ref_commit("refs/tags/" + name)

    def _ref_commit(self, ref: str) -> str | None:
        result = subprocess.run(
            ["git", "show-ref", "--verify", "--quiet", ref],
            cwd=self.root,
            capture_output=True,
            text=True,
            timeout=30,
            check=False,
        )
        if result.returncode == 1:
            return None
        if result.returncode:
            raise ValueError("could not inspect Git ref")
        return git(self.root, "rev-parse", ref + "^{commit}").strip()


@dataclass(frozen=True, slots=True)
class Preparation:
    """Reviewable plugin preparation from a version already calculated by the pipeline."""

    candidate: Candidate
    changes: tuple[FileChange, ...]
    branch: str
    base_branch: str
    title: str
    body: str


def prepare(
    source: GitSource,
    candidate: Candidate,
    base_branch: str,
) -> Preparation:
    """Plan one new metadata branch; the workflow owns branch and PR collision checks."""
    source.verify(candidate.source_sha)
    verify_tag(candidate, source.tag(candidate.tag))
    sources = {path: source.read(candidate.source_sha, path) for path in FILES}
    historical = (
        object_source(
            {"versions": source.read(candidate.baseline, "plugin/versions.json")},
            "versions",
        )
        if candidate.baseline
        else {}
    )
    changes = prepare_sources(sources, candidate, historical)
    branch = "release/plugin-" + candidate.version
    body = f"Prepare plugin {candidate.version} from frozen source `{candidate.source_sha}`.\n\nSynchronize plugin metadata. Preserve minimum supported Obsidian versions for previous releases.\n\n<!-- topic-stream-plugin-preparation {candidate.version} {candidate.source_sha} -->\n"
    plan = Preparation(
        candidate,
        changes,
        branch,
        base_branch,
        f"chore(plugin): prepare {candidate.version}",
        body,
    )
    return plan


def emit(value: object) -> None:
    text = (
        json.dumps(
            dataclasses.asdict(value)
            if dataclasses.is_dataclass(value) and not isinstance(value, type)
            else value,
            indent=2,
        )
        + "\n"
    )
    print(text, end="")


def outputs(path: Path | None, values: Mapping[str, str]) -> None:
    if path:
        with path.open("a") as output:
            for name, value in values.items():
                if "\n" in value or "\r" in value:
                    raise ValueError("workflow output must be a single line")
                output.write(f"{name}={value}\n")


def parser() -> argparse.ArgumentParser:
    result = argparse.ArgumentParser(description=__doc__)
    result.add_argument("--version", required=True)
    result.add_argument("--baseline", default="")
    result.add_argument("--base-branch", default="main")
    result.add_argument("--sha", required=True)
    result.add_argument("--body-output", type=Path)
    result.add_argument("--github-output", type=Path)
    result.add_argument(
        "--apply",
        action="store_true",
        help="write only the planned local plugin metadata",
    )
    return result


def main(argv: Sequence[str] | None = None, *, root: Path | None = None) -> int:
    args = parser().parse_args(argv)
    source = GitSource(root or Path.cwd())
    try:
        version(args.version)
        if args.baseline and version(args.version) <= version(args.baseline):
            raise ValueError("plugin candidate must be newer than the published baseline")
        candidate = Candidate(
            "plugin", args.version, args.version, args.baseline or None, args.sha
        )
        plan = prepare(
            source,
            candidate,
            args.base_branch,
        )
        if args.apply:
            if any(
                (source.root / change.path).read_text()
                != source.read(args.sha, change.path)
                for change in plan.changes
            ):
                raise ValueError(
                    "worktree metadata differs from the frozen source; no edits applied"
                )
            for change in plan.changes:
                (source.root / change.path).write_text(change.contents)
        if args.body_output:
            args.body_output.parent.mkdir(parents=True, exist_ok=True)
            args.body_output.write_text(plan.body)
        emit(plan)
        outputs(
            args.github_output,
            {
                "branch": plan.branch,
                "version": candidate.version,
                "title": plan.title,
                "changed": str(bool(plan.changes)).lower(),
            },
        )
    except (TypeError, ValueError, OSError, subprocess.SubprocessError) as error:
        print(
            f"Plugin preparation failed: {error if isinstance(error, (TypeError, ValueError)) else type(error).__name__}",
            file=sys.stderr,
        )
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
