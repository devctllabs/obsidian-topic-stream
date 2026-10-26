#!/usr/bin/env python3
"""Compute one component release candidate from a workflow-owned history snapshot."""

from __future__ import annotations

import argparse
import dataclasses
import json
import re
import subprocess
import sys
from collections.abc import Mapping, Sequence
from pathlib import Path

from release_policy import calculate_candidate, history, latest, verify_tag


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


class GitSource:
    """Inspect only the immutable checkout and already fetched local refs."""

    def __init__(self, root: Path):
        self.root = root

    def verify(self, sha: str) -> None:
        if (
            not re.fullmatch(r"[0-9a-f]{40}", sha)
            or git(self.root, "rev-parse", "HEAD").strip() != sha
        ):
            raise ValueError("release requires the exact checked-out full source SHA")

    def changed(self, baseline: str | None, sha: str) -> tuple[str, ...]:
        arguments = (
            ("diff", "--no-renames", "--name-only", "-z", baseline, sha, "--")
            if baseline
            else ("ls-tree", "-r", "--name-only", "-z", sha)
        )
        return tuple(path for path in git(self.root, *arguments).split("\0") if path)

    def tag(self, name: str) -> str | None:
        ref = "refs/tags/" + name
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
            raise ValueError("could not inspect candidate tag")
        return git(self.root, "rev-parse", ref + "^{commit}").strip()


def emit(value: object, output: Path | None) -> None:
    text = (
        json.dumps(
            dataclasses.asdict(value)
            if dataclasses.is_dataclass(value) and not isinstance(value, type)
            else value,
            indent=2,
        )
        + "\n"
    )
    if output:
        output.parent.mkdir(parents=True, exist_ok=True)
        output.write_text(text)
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
    result.add_argument("--component", choices=("backend", "plugin"), required=True)
    result.add_argument("--bump", choices=("major", "minor", "patch"), required=True)
    result.add_argument("--history", type=Path, required=True)
    result.add_argument("--sha", required=True)
    result.add_argument("--output", type=Path)
    result.add_argument("--github-output", type=Path)
    return result


def main(argv: Sequence[str] | None = None, *, root: Path | None = None) -> int:
    args = parser().parse_args(argv)
    source = GitSource(root or Path.cwd())
    try:
        source.verify(args.sha)
        releases = history(json.loads(args.history.read_text()))
        baseline = latest(args.component, releases)
        candidate = calculate_candidate(
            args.component,
            args.bump,
            args.sha,
            releases,
            source.changed(baseline.tag if baseline else None, args.sha),
        )
        verify_tag(candidate, source.tag(candidate.tag))
        emit(candidate, args.output)
        outputs(
            args.github_output,
            {
                "version": candidate.version,
                "tag": candidate.tag,
                "baseline": candidate.baseline or "",
                "source_sha": candidate.source_sha,
            },
        )
    except (TypeError, ValueError, OSError, subprocess.SubprocessError) as error:
        print(
            f"Release calculation failed: {error if isinstance(error, (TypeError, ValueError)) else type(error).__name__}",
            file=sys.stderr,
        )
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
