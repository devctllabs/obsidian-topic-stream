"""Run explicit local capacity/live checks against disposable, capped services."""

from __future__ import annotations

import argparse
import dataclasses
import json
import os
import subprocess
import sys
import threading
import time
import urllib.error
import urllib.request
import uuid
from collections.abc import Mapping, Sequence
from dataclasses import dataclass
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Literal, Protocol, Self
from urllib.parse import urlsplit


@dataclass(frozen=True, slots=True)
class Resource:
    """One declared local service budget, independent of a pre-existing environment."""

    name: str
    image: str
    cpus: int
    memory_gib: int
    ports: tuple[int, ...]


@dataclass(frozen=True, slots=True)
class Profile:
    """A maintained oracle and the package harness resources needed to run it."""

    package: str
    test: str
    tags: str
    services: tuple[str, ...]
    kind: Literal["sql", "synthetic", "embedding", "summary", "checks"]


RESOURCES = {
    "postgres": Resource("postgres", "postgres:17-alpine", 2, 4, (5432,)),
    "qdrant": Resource("qdrant", "qdrant/qdrant:v1.20", 2, 6, (6333, 6334)),
}


def sql_profile(package: str, test: str) -> Profile:
    """Include the Qdrant health companion required by the deps package TestMain."""
    services = ("postgres", "qdrant") if package == "deps" else ("postgres",)
    return Profile(
        f"./internal/{package}", test, "integration,performance", services, "sql"
    )


PROFILES = {
    "sql-http": sql_profile("deps", "TestDatabasePerformanceHTTP"),
    "sql-capture": sql_profile("deps", "TestCaptureMatrixCapacity"),
    "sql-ack": sql_profile("deps", "TestProjectionACKMatrixCapacity"),
    "sql-model-switch": sql_profile("deps", "TestModelSwitchingCapacity"),
    "sql-repair": sql_profile("deps", "TestProjectionRepairCapacity"),
    "sql-feed-adversarial": sql_profile("deps", "TestFeedAdversarialCapacity"),
    "sql-model-delta": sql_profile(
        "repository/postgres", "TestRetainedModelDeltaCapacity"
    ),
    "sql-candidates": sql_profile("repository/postgres", "TestCandidateCapacity"),
    "sql-digests": sql_profile("repository/postgres", "TestDigestHistoryCapacity"),
    "sql-feed-plan": sql_profile(
        "repository/postgres",
        "TestFeedDisplayRefreshBoundsCorrectionWorkAfterWorkspaceTopicGrowth",
    ),
    "qdrant-search": Profile(
        "./internal/platform/vectorindex",
        "TestSyntheticQdrantSearchCapacity",
        "integration,performance",
        ("qdrant",),
        "synthetic",
    ),
    "qdrant-ensure": Profile(
        "./internal/platform/vectorindex",
        "TestQdrantEnsureExistingCollectionCapacity",
        "integration,performance",
        ("qdrant",),
        "checks",
    ),
    "qdrant-checks": Profile(
        "./internal/platform/vectorindex",
        "TestSynthetic(Recall|Policy|Result|Optimization|CapacityVectors).*",
        "integration,performance",
        ("qdrant",),
        "checks",
    ),
    "math-checks": Profile(
        "./internal/testutil",
        "TestPercentiles.*",
        "integration,performance",
        (),
        "checks",
    ),
    "live-index": Profile(
        "./internal/deps",
        "TestLiveRetrievalPilotCapacity",
        "integration,live",
        ("postgres", "qdrant"),
        "embedding",
    ),
    "live-lifecycle": Profile(
        "./internal/deps",
        "TestLiveRetrievalLifecycleCapacity",
        "integration,live",
        ("postgres", "qdrant"),
        "embedding",
    ),
    "live-summary": Profile(
        "./internal/repository/postgres",
        "TestSummaryLiveProvider",
        "integration,live",
        ("postgres",),
        "summary",
    ),
    "live-observer-checks": Profile(
        "./internal/deps",
        "TestLivePilotEmbeddingObserverCountsBatchInputsSeparatelyFromRequests",
        "integration,live",
        ("postgres", "qdrant"),
        "checks",
    ),
}


@dataclass(frozen=True, slots=True)
class Plan:
    """Validated invocation; provider secrets and inherited service URLs are absent."""

    name: str
    profile: Profile
    resources: tuple[Resource, ...]
    run_id: str
    output: Path
    smoke: bool
    points: int | None
    distribution: str | None
    items: int | None
    endpoint: str | None
    model: str | None
    dimensions: int | None


@dataclass(frozen=True, slots=True)
class CommandResult:
    """Safe process outcome; full Go output belongs in its requested log file."""

    returncode: int
    stdout: str = ""
    elapsed_seconds: float = 0


class LocalRuntime(Protocol):
    """External process/probe capability consumed by the local profile lifecycle."""

    def run(
        self,
        argv: Sequence[str],
        *,
        env: Mapping[str, str] | None = None,
        output: Path | None = None,
        timeout: int = 120,
    ) -> CommandResult:
        """Run a bounded process, optionally directing its output to an owned report."""
        ...

    def wait_ready(
        self, resource: Resource, name: str, endpoint: str, timeout: int
    ) -> bool:
        """Observe one local service until success or the fixed deadline."""
        ...


class ExecutionError(RuntimeError):
    """A failed local process or resource lifecycle operation."""


class SubprocessRuntime:
    """Own bounded process execution and local readiness probes; never use a shell."""

    def __init__(self, root: Path):
        self.root = root

    def run(
        self,
        argv: Sequence[str],
        *,
        env: Mapping[str, str] | None = None,
        output: Path | None = None,
        timeout: int = 120,
    ) -> CommandResult:
        started = time.perf_counter()
        try:
            if output is None:
                process = subprocess.run(
                    argv,
                    cwd=self.root,
                    env=env,
                    text=True,
                    stdout=subprocess.PIPE,
                    stderr=subprocess.STDOUT,
                    timeout=timeout,
                    check=False,
                )
                body = process.stdout
            else:
                with output.open("w") as log:
                    output.chmod(0o600)
                    process = subprocess.run(
                        argv,
                        cwd=self.root,
                        env=env,
                        text=True,
                        stdout=log,
                        stderr=subprocess.STDOUT,
                        timeout=timeout,
                        check=False,
                    )
                body = ""
        except (OSError, subprocess.TimeoutExpired) as error:
            raise ExecutionError(
                f"{argv[0]} execution failed ({type(error).__name__})"
            ) from error
        return CommandResult(process.returncode, body, time.perf_counter() - started)

    def wait_ready(
        self, resource: Resource, name: str, endpoint: str, timeout: int
    ) -> bool:
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            if resource.name == "postgres":
                if (
                    self.run(
                        [
                            "docker",
                            "exec",
                            name,
                            "pg_isready",
                            "-h",
                            "127.0.0.1",
                            "-U",
                            "topic_stream",
                        ],
                        timeout=5,
                    ).returncode
                    == 0
                ):
                    return True
            else:
                try:
                    with urllib.request.urlopen(
                        endpoint + "/readyz", timeout=1
                    ) as response:
                        if response.status == 200:
                            return True
                except (OSError, urllib.error.URLError):
                    pass
            time.sleep(0.2)
        return False


@dataclass(frozen=True, slots=True)
class OwnedResource:
    """An attempted creation, checked by its private run label before removal."""

    name: str
    created: bool


def checked(runtime: LocalRuntime, argv: Sequence[str]) -> str:
    """Normalize command failures without printing environment or full arguments."""
    result = runtime.run(argv)
    if result.returncode:
        raise ExecutionError(f"{argv[0]} command failed (exit {result.returncode})")
    return result.stdout.strip()


def docker_arguments(resource: Resource, name: str, run_id: str) -> list[str]:
    """Declare fixed caps, loopback-only ephemeral ports and explicit ownership."""
    command = [
        "docker",
        "run",
        "--detach",
        "--rm",
        "--name",
        name,
        "--label",
        f"topic-stream.performance-run={run_id}",
        "--cpus",
        str(resource.cpus),
        "--memory",
        f"{resource.memory_gib}g",
    ]
    for port in resource.ports:
        command += ["--publish", f"127.0.0.1::{port}"]
    if resource.name == "postgres":
        command += [
            "--shm-size",
            "1g",
            "--env",
            "POSTGRES_USER=topic_stream",
            "--env",
            "POSTGRES_PASSWORD=benchmark",
            "--env",
            "POSTGRES_DB=topic_stream",
        ]
    command += [resource.image]
    if resource.name == "postgres":
        command += [
            "postgres",
            "-c",
            "shared_buffers=1GB",
            "-c",
            "max_wal_size=2GB",
            "-c",
            "checkpoint_timeout=30min",
            "-c",
            "shared_preload_libraries=pg_stat_statements",
            "-c",
            "pg_stat_statements.track=all",
        ]
    return command


def start_resources(
    plan: Plan, runtime: LocalRuntime, env: dict[str, str], owned: list[OwnedResource]
) -> None:
    """Record partial creations before launch; configure only verified owned endpoints."""
    for resource in plan.resources:
        prefix = (
            "topic-stream-postgres-perf-"
            if resource.name == "postgres"
            else "topic-stream-qdrant-search-perf-"
        )
        name = prefix + plan.run_id[:16]
        owned.append(OwnedResource(name, False))
        checked(runtime, docker_arguments(resource, name, plan.run_id))
        owned[-1] = OwnedResource(name, True)
        ports = {
            port: checked(runtime, ["docker", "port", name, f"{port}/tcp"])
            for port in resource.ports
        }
        if resource.name == "postgres":
            env["TOPIC_STREAM_TEST_POSTGRES_DSN"] = (
                f"postgres://topic_stream:benchmark@{ports[5432]}/topic_stream?sslmode=disable"
            )
            endpoint = ""
        else:
            endpoint = "http://" + ports[6333]
            env.update(
                {
                    "TOPIC_STREAM_TEST_QDRANT_URL": "http://" + ports[6334],
                    "TOPIC_STREAM_PERF_QDRANT_HTTP": endpoint,
                    "TOPIC_STREAM_SYNTHETIC_QDRANT_HTTP": endpoint,
                    "TOPIC_STREAM_PILOT_QDRANT_HTTP": endpoint,
                    "TOPIC_STREAM_PERF_QDRANT_CONTAINER": name,
                    "TOPIC_STREAM_SYNTHETIC_QDRANT_CONTAINER": name,
                }
            )
        if not runtime.wait_ready(resource, name, endpoint, 45):
            raise ExecutionError(
                f"{resource.name} did not become ready within 45 seconds"
            )


def cleanup_resources(
    plan: Plan, runtime: LocalRuntime, owned: Sequence[OwnedResource]
) -> list[str]:
    """Remove only matching-label resources, including a partially failed creation."""
    errors: list[str] = []
    for resource in reversed(owned):
        try:
            observed = runtime.run(
                [
                    "docker",
                    "inspect",
                    "--format",
                    '{{index .Config.Labels "topic-stream.performance-run"}}',
                    resource.name,
                ],
                timeout=15,
            )
            if observed.returncode:
                if resource.created:
                    errors.append(f"could not inspect owned resource {resource.name}")
                continue
            if observed.stdout.strip() != plan.run_id:
                errors.append(
                    f"ownership differs; resource left untouched: {resource.name}"
                )
                continue
            result = runtime.run(
                ["docker", "rm", "--force", "--volumes", resource.name], timeout=30
            )
            if result.returncode:
                errors.append(f"could not remove owned resource {resource.name}")
        except ExecutionError as error:
            errors.append(str(error))
    return errors


def test_environment(plan: Plan, inherited: Mapping[str, str]) -> dict[str, str]:
    """Do not reuse operator databases or implicitly enable another live profile."""
    env = dict(inherited)
    for key in (
        "TOPIC_STREAM_TEST_POSTGRES_DSN",
        "TOPIC_STREAM_TEST_QDRANT_URL",
        "TOPIC_STREAM_PILOT_OLLAMA",
        "TOPIC_STREAM_PILOT_MODEL",
        "TOPIC_STREAM_LIVE_SUMMARY_ENDPOINT",
        "TOPIC_STREAM_LIVE_SUMMARY_MODEL",
    ):
        env.pop(key, None)
    env.update(
        {
            "TOPIC_STREAM_PERF_RUN_ID": plan.run_id,
            "TOPIC_STREAM_PERF_REPORT": str(plan.output / "report.json"),
            "TOPIC_STREAM_CAPTURE_STATEMENTS": "true",
            "TOPIC_STREAM_CAPTURE_FRESH_KEYS": "true",
        }
    )
    if plan.profile.kind == "synthetic":
        env.update(
            {
                "TOPIC_STREAM_SYNTHETIC_POINTS": str(plan.points),
                "TOPIC_STREAM_SYNTHETIC_DISTRIBUTION": plan.distribution or "dominant",
            }
        )
    if plan.profile.kind == "embedding":
        env.update(
            {
                "TOPIC_STREAM_PILOT_OLLAMA": plan.endpoint or "",
                "TOPIC_STREAM_PILOT_MODEL": plan.model or "",
                "TOPIC_STREAM_PILOT_ITEMS": str(plan.items),
                "TOPIC_STREAM_PILOT_DIMENSIONS": str(plan.dimensions),
                "TOPIC_STREAM_PILOT_BATCH_SIZE": "8",
                "TOPIC_STREAM_PILOT_CONCURRENCY": "1",
                "TOPIC_STREAM_PILOT_LIFECYCLE": str(
                    plan.name == "live-lifecycle"
                ).lower(),
                "TOPIC_STREAM_LIVE_SMOKE": str(plan.smoke).lower(),
            }
        )
    if plan.profile.kind == "summary":
        env.update(
            {
                "TOPIC_STREAM_LIVE_SUMMARY_ENDPOINT": plan.endpoint or "",
                "TOPIC_STREAM_LIVE_SUMMARY_MODEL": plan.model or "",
            }
        )
    return env


def execute(plan: Plan, runtime: LocalRuntime, inherited: Mapping[str, str]) -> int:
    """Run one oracle and always finish label-checked cleanup before reporting success."""
    plan.output.mkdir(parents=True, exist_ok=True, mode=0o700)
    environment = test_environment(plan, inherited)
    owned: list[OwnedResource] = []
    status, failure, elapsed = 1, None, 0.0
    try:
        start_resources(plan, runtime, environment, owned)
        command = [
            "go",
            "test",
            "-count=1",
            "-p=1",
            "-json",
            "-tags=" + plan.profile.tags,
            "-timeout=90m",
            "-run",
            "^" + plan.profile.test + "$",
            plan.profile.package,
        ]
        outcome = runtime.run(
            command, env=environment, output=plan.output / "go-test.jsonl", timeout=5500
        )
        status, elapsed = outcome.returncode, outcome.elapsed_seconds
    except ExecutionError as error:
        failure = str(error)
    except KeyboardInterrupt:
        failure, status = "interrupted", 130
    finally:
        cleanup = cleanup_resources(plan, runtime, owned)
        if cleanup:
            status = 1
            failure = failure or "owned resource cleanup failed"
        record = {
            "plan": plan,
            "status": "complete" if status == 0 else "failed",
            "test_elapsed_seconds": elapsed,
            "failure": failure,
            "cleanup_errors": cleanup,
        }
        report = plan.output / "run.json"
        report.write_text(json.dumps(record, default=json_value, indent=2) + "\n")
        report.chmod(0o600)
    if status:
        print(
            f"performance: {failure or 'selected test failed'}; reports: {plan.output}",
            file=sys.stderr,
        )
    else:
        print(f"reports: {plan.output}")
    return status if status >= 0 else 1


class SmokeProvider:
    """A loopback-only OpenAI protocol stub for explicit smoke mode, never model evidence."""

    def __init__(self, dimensions: int):
        self.dimensions = dimensions
        provider = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, format: str, *args: object) -> None:
                pass

            def do_POST(self) -> None:
                length = int(self.headers.get("Content-Length", "0"))
                if not 0 < length <= 4 << 20:
                    self.send_error(400)
                    return
                try:
                    body: object = json.loads(self.rfile.read(length))
                except (ValueError, OSError):
                    self.send_error(400)
                    return
                if not isinstance(body, dict):
                    self.send_error(400)
                    return
                if self.path == "/v1/embeddings":
                    inputs = body.get("input")
                    if isinstance(inputs, str):
                        inputs = [inputs]
                    if not isinstance(inputs, list) or not all(
                        isinstance(text, str) for text in inputs
                    ):
                        self.send_error(400)
                        return
                    dimensions = body.get("dimensions", provider.dimensions)
                    if type(dimensions) is not int or not 1 <= dimensions <= 65536:
                        self.send_error(400)
                        return
                    vector = [1.0] + [0.0] * (dimensions - 1)
                    result = {
                        "data": [
                            {"index": index, "embedding": vector}
                            for index in range(len(inputs))
                        ],
                        "usage": {
                            "prompt_tokens": sum(len(text) for text in inputs) // 4
                        },
                    }
                elif self.path == "/v1/chat/completions":
                    result = {
                        "choices": [
                            {
                                "message": {
                                    "content": json.dumps(
                                        {
                                            "text": "Controlled synthetic smoke annotation; no model was called."
                                        }
                                    )
                                },
                                "finish_reason": "stop",
                            }
                        ]
                    }
                else:
                    self.send_error(404)
                    return
                payload = json.dumps(result).encode()
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(payload)))
                self.end_headers()
                self.wfile.write(payload)

        self.server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.thread = threading.Thread(
            target=self.server.serve_forever,
            kwargs={"poll_interval": 0.02},
            daemon=True,
        )
        self.endpoint = f"http://127.0.0.1:{self.server.server_port}"

    def __enter__(self) -> Self:
        self.thread.start()
        return self

    def __exit__(
        self, exception_type: object, exception: object, traceback: object
    ) -> None:
        self.server.shutdown()
        self.server.server_close()
        self.thread.join(timeout=3)


def parser() -> argparse.ArgumentParser:
    """Describe explicit local profiles without opening a service or creating reports."""
    result = argparse.ArgumentParser(description=__doc__)
    result.add_argument(
        "profile", choices=sorted(PROFILES), help="one maintained local oracle"
    )
    result.add_argument(
        "--dry-run",
        action="store_true",
        help="print validated inputs/resources without execution",
    )
    result.add_argument(
        "--smoke",
        action="store_true",
        help="1024 synthetic vectors or 3 stub-provider Items; no real model",
    )
    result.add_argument(
        "--points",
        type=int,
        choices=(100000, 1000000),
        help="synthetic document vectors; defaults to 100000",
    )
    result.add_argument(
        "--distribution",
        choices=("balanced", "dominant"),
        help="synthetic Workspace distribution; default dominant (80%%)",
    )
    result.add_argument(
        "--endpoint",
        help="live embedding server root, or Summary OpenAI-compatible base URL",
    )
    result.add_argument("--model", help="explicit installed live provider model")
    result.add_argument(
        "--dimensions", type=int, help="embedding vector dimensions; default 1024"
    )
    result.add_argument(
        "--output",
        type=Path,
        help="private report directory; default .artifacts/performance/<run>",
    )
    return result


def live_inputs(
    profile: Profile, args: argparse.Namespace, env: Mapping[str, str]
) -> tuple[str | None, str | None]:
    """Require explicit non-secret live inputs; synthetic selection never consumes them."""
    if profile.kind not in ("embedding", "summary") or args.smoke:
        return None, None
    prefix = (
        "TOPIC_STREAM_PILOT_"
        if profile.kind == "embedding"
        else "TOPIC_STREAM_LIVE_SUMMARY_"
    )
    endpoint_key = prefix + ("OLLAMA" if profile.kind == "embedding" else "ENDPOINT")
    endpoint = (args.endpoint or env.get(endpoint_key, "")).strip().rstrip("/")
    model = (args.model or env.get(prefix + "MODEL", "")).strip()
    if not endpoint or not model:
        raise ValueError("live execution requires an explicit --endpoint and --model")
    parsed = urlsplit(endpoint)
    if (
        parsed.scheme not in ("http", "https")
        or not parsed.hostname
        or parsed.username
        or parsed.password
        or parsed.query
        or parsed.fragment
    ):
        raise ValueError(
            "provider endpoint must be an HTTP(S) URL without credentials, query or fragment"
        )
    if profile.kind == "embedding" and parsed.path:
        raise ValueError(
            "embedding endpoint must be the server root; /v1/embeddings is appended"
        )
    return endpoint, model


def build_plan(
    args: argparse.Namespace, env: Mapping[str, str], root: Path, run_id: str
) -> Plan:
    """Resolve bounded work before constructing resources; reject ambiguous smoke requests."""
    profile = PROFILES[args.profile]
    if args.smoke and (
        args.points is not None
        or profile.kind not in ("synthetic", "embedding", "summary")
    ):
        raise ValueError(
            "smoke supports synthetic/live profiles and cannot combine an explicit point count"
        )
    if args.points is not None and profile.kind != "synthetic":
        raise ValueError(
            "--points applies only to synthetic vectors, never real-provider Items"
        )
    endpoint, model = live_inputs(profile, args, env)
    dimensions = args.dimensions if args.dimensions is not None else 1024
    if not 1 <= dimensions <= 65536:
        raise ValueError("embedding dimensions must be between 1 and 65536")
    if profile.kind == "synthetic" and dimensions != 1024:
        raise ValueError("synthetic corpus has fixed 1024-dimensional vectors")
    return Plan(
        args.profile,
        profile,
        tuple(RESOURCES[name] for name in profile.services),
        run_id,
        (
            args.output or root / ".artifacts" / "performance" / run_id / args.profile
        ).resolve(),
        args.smoke,
        (1024 if args.smoke else args.points or 100000)
        if profile.kind == "synthetic"
        else None,
        (args.distribution or "dominant") if profile.kind == "synthetic" else None,
        (3 if args.smoke else 500) if profile.kind == "embedding" else None,
        endpoint,
        model,
        dimensions if profile.kind in ("embedding", "synthetic") else None,
    )


def json_value(value: object) -> object:
    """Serialize typed CLI values only at the report boundary."""
    if isinstance(value, Path):
        return str(value)
    if dataclasses.is_dataclass(value) and not isinstance(value, type):
        return dataclasses.asdict(value)
    raise TypeError(f"unsupported report value {type(value).__name__}")


def main(
    argv: Sequence[str] | None = None,
    *,
    env: Mapping[str, str] | None = None,
    runtime: LocalRuntime | None = None,
) -> int:
    """Run one profile; invalid inputs and dry-run never create containers or report files."""
    arguments = parser().parse_args(argv)
    environment = dict(os.environ if env is None else env)
    try:
        plan = build_plan(
            arguments,
            environment,
            Path(__file__).resolve().parent.parent,
            uuid.uuid4().hex,
        )
    except ValueError as error:
        print(f"performance: {error}", file=sys.stderr)
        return 2
    if arguments.dry_run:
        print(json.dumps(plan, default=json_value, indent=2))
        return 0
    adapter = runtime or SubprocessRuntime(Path(__file__).resolve().parent.parent)
    try:
        if plan.smoke and plan.profile.kind in ("embedding", "summary"):
            with SmokeProvider(plan.dimensions or 1024) as provider:
                endpoint = provider.endpoint + (
                    "/v1" if plan.profile.kind == "summary" else ""
                )
                return execute(
                    dataclasses.replace(
                        plan, endpoint=endpoint, model="synthetic-smoke"
                    ),
                    adapter,
                    environment,
                )
        return execute(plan, adapter, environment)
    except OSError as error:
        print(
            f"performance: report or local runtime I/O failed ({type(error).__name__})",
            file=sys.stderr,
        )
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
