# Runtime and status

Topic Stream ships one `topic-stream serve` binary with independently enabled HTTP, scheduler, and worker
roles. All roles are enabled by default for an all-in-one process. HTTP and scheduler replicas use
the Workspace selected by `TOPIC_STREAM_WORKSPACE_ID`; worker-only replicas omit it and serve Jobs from
all Workspaces.

The daemon listens on `127.0.0.1:7331` by default. `GET /api/v1/system/status` returns the daemon version, workspace identifier, readiness state, component statuses, and server time. PostgreSQL and Qdrant are checked with bounded live probes. PostgreSQL failure makes the daemon `unavailable`; Qdrant failure makes it `degraded` because Qdrant is rebuildable derived state. Probe responses expose only a stable failure detail and never raw connection errors or credentials.

The daemon can start while Qdrant is unavailable so database-backed status, Embedding Model administration, settings, retrieval status, and Jobs remain usable. Embedding Model initialization failures remain inspectable and retryable. Retrieval is unavailable only when no Serving Embedding Model is ready; an unsuccessful Model Transition leaves the previous model serving. When optional generation is enabled, status probes its OpenAI-compatible `/models` endpoint and reports failure as degraded; disabled generation is omitted. The probe reads current Workspace Settings, so toggling generation does not require a daemon restart.

On interruption or termination, the shared lifecycle cancels only the roles enabled in that
process, then releases Qdrant and PostgreSQL resources within `TOPIC_STREAM_SHUTDOWN_TIMEOUT`. A worker
stops leasing new work, cooperatively cancels owned execution, and joins its pools before release.

See [server configuration](../../operations/configuration.md) for full-stack and development
deployments, environment variables, and model bootstrap.

Queue layout, role flags, and cross-process invalidation are described in
[Jobs and runtime roles](../../architecture/jobs-and-runtime-roles.md).

Serving retrieval follows durable model state across independently running roles, even after missed
notifications. Each operation uses one model/endpoint/index snapshot. Retrieval status reports no
ready Serving model for a lost index instance until recovery completes; database-backed Feed reads,
settings and Jobs remain available. Search returns `503 retrieval_unavailable` during that recovery.
