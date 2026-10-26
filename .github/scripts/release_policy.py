"""Pure component release policy; external workflows own I/O and side effects."""

from __future__ import annotations

import re
from collections.abc import Sequence
from dataclasses import dataclass


NON_PRODUCT_PREFIXES = (".github/", ".scratch/", "docs/")
NON_PRODUCT_PATHS = {
    "AGENTS.md",
    "CONTEXT.md",
    "DESIGN.md",
    "PRODUCT.md",
    "LICENSE",
    ".gitignore",
    ".mise.toml",
}
BACKEND_PREFIXES = ("cmd/", "internal/", "gen/", "migrations/", "tools/")
BACKEND_PATHS = {
    ".dockerignore",
    ".env.example",
    ".golangci.yml",
    "Dockerfile",
    "compose.yaml",
    "compose.dev.yaml",
    "go.mod",
    "go.sum",
}
PLUGIN_PREFIXES = ("plugin/",)
PLUGIN_PATHS = {"package.json", "pnpm-lock.yaml", "pnpm-workspace.yaml"}
SHARED_PREFIXES = ("api/openapi/", "tools/oapi/")
SHARED_PATHS = {"devctl.yaml", "redocly.yaml"}


@dataclass(frozen=True, slots=True)
class Release:
    """Stable-history metadata; drafts and prereleases are excluded from the baseline."""

    tag: str
    draft: bool = False
    prerelease: bool = False


@dataclass(frozen=True, slots=True)
class Candidate:
    """One explicit component version bound to its immutable source commit."""

    component: str
    version: str
    tag: str
    baseline: str | None
    source_sha: str


def version(value: str) -> tuple[int, int, int]:
    """Parse only canonical stable SemVer; prerelease and leading-zero forms fail."""
    if not re.fullmatch(r"(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)", value):
        raise ValueError(f"invalid stable version: {value}")
    major, minor, patch = value.split(".")
    return int(major), int(minor), int(patch)


def tag_version(tag: str, component: str) -> tuple[int, int, int] | None:
    """Ignore other namespaces and reject malformed published tags in this namespace."""
    if component == "backend":
        if not tag.startswith("backend/"):
            return None
        if not tag.startswith("backend/v"):
            raise ValueError("malformed published backend tag")
        return version(tag.removeprefix("backend/v"))
    if not re.match(r"[0-9]", tag):
        return None
    return version(tag)


def latest(component: str, history: Sequence[Release]) -> Release | None:
    """Find the highest published stable version independently of publication date."""
    choices = []
    for release in history:
        if release.draft or release.prerelease:
            continue
        parsed = tag_version(release.tag, component)
        if parsed is not None:
            choices.append((parsed, release))
    return max(choices, key=lambda choice: choice[0])[1] if choices else None


def rows(value: object) -> list[dict[str, object]]:
    """Decode plain arrays or paginated gh --slurp snapshots at the JSON boundary."""
    if not isinstance(value, list):
        raise TypeError("release snapshot must be an array")
    flattened = (
        [row for page in value for row in page]
        if value and all(isinstance(page, list) for page in value)
        else value
    )
    if any(not isinstance(row, dict) for row in flattened):
        raise ValueError("release snapshot contains malformed records")
    return flattened


def history(value: object) -> tuple[Release, ...]:
    """Require typed history; an API failure or malformed file is never empty history."""
    result = []
    for row in rows(value):
        if (
            not isinstance(row.get("tag_name"), str)
            or type(row.get("draft")) is not bool
            or type(row.get("prerelease")) is not bool
        ):
            raise ValueError(
                "release history requires typed tag_name, draft and prerelease"
            )
        result.append(Release(row["tag_name"], row["draft"], row["prerelease"]))
    return tuple(result)


def path_owner(path: str) -> str | None:
    """Classify product paths; unclassified paths fail before a release is planned."""
    if path.startswith(NON_PRODUCT_PREFIXES) or path in NON_PRODUCT_PATHS:
        return None
    if path.startswith(SHARED_PREFIXES) or path in SHARED_PATHS:
        return "shared"
    if path.startswith(PLUGIN_PREFIXES) or path in PLUGIN_PATHS:
        return "plugin"
    if path.startswith(BACKEND_PREFIXES) or path in BACKEND_PATHS:
        return "backend"
    raise ValueError(f"release path policy has no owner: {path}")


def component_relevant(paths: Sequence[str], component: str) -> bool:
    """Return whether a component has product changes in the supplied committed paths."""
    owners = {owner for path in paths if (owner := path_owner(path)) is not None}
    return component in owners or "shared" in owners


def calculate_candidate(
    component: str,
    bump: str,
    sha: str,
    history: Sequence[Release],
    changed: Sequence[str],
) -> Candidate:
    """Require relevant work, then apply an explicit bump to component history."""
    if component not in ("backend", "plugin") or bump not in (
        "major",
        "minor",
        "patch",
    ):
        raise ValueError(
            "release requires a supported component and explicit stable bump"
        )
    if not re.fullmatch(r"[0-9a-f]{40}", sha) or sha == "0" * 40:
        raise ValueError("release requires an immutable full source SHA")
    if not component_relevant(changed, component):
        raise ValueError(f"no new relevant {component} changes")
    baseline = latest(component, history)
    numbers = (0, 1, 0)
    if baseline is not None:
        old = tag_version(baseline.tag, component)
        assert old is not None
        major, minor, patch = old
        numbers = {
            "major": (major + 1, 0, 0),
            "minor": (major, minor + 1, 0),
            "patch": (major, minor, patch + 1),
        }[bump]
    result = ".".join(str(part) for part in numbers)
    tag = "backend/v" + result if component == "backend" else result
    return Candidate(component, result, tag, baseline.tag if baseline else None, sha)


def verify_tag(candidate: Candidate, existing_sha: str | None) -> None:
    """An existing candidate tag may never be moved to another source commit."""
    if existing_sha is not None and existing_sha != candidate.source_sha:
        raise ValueError(
            f"candidate tag {candidate.tag} already identifies another source"
        )
