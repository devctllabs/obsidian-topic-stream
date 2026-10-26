CREATE TABLE jobs (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    kind text NOT NULL,
    queue text NOT NULL CONSTRAINT jobs_queue_check CHECK (
        queue IN ('fetch', 'generation', 'retrieval', 'maintenance', 'feed-preparation')
    ),
    args_version integer NOT NULL DEFAULT 1 CONSTRAINT jobs_args_version_check CHECK (args_version >= 1),
    payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    idempotency_key text,
    status text NOT NULL DEFAULT 'queued' CONSTRAINT jobs_status_check CHECK (
        status IN ('queued', 'running', 'retry_wait', 'succeeded', 'dead', 'cancelled')
    ),
    attempt integer NOT NULL DEFAULT 0,
    max_attempts integer NOT NULL DEFAULT 3,
    available_at timestamptz NOT NULL DEFAULT now(),
    lease_owner text,
    lease_expires_at timestamptz,
    cancellation_requested boolean NOT NULL DEFAULT false,
    reason text,
    created_at timestamptz NOT NULL DEFAULT now(),
    started_at timestamptz,
    finished_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT jobs_workspace_id_id_key UNIQUE (workspace_id,id)
);

CREATE INDEX jobs_claim_idx ON jobs (queue, available_at, created_at, id)
    WHERE status IN ('queued', 'retry_wait');
CREATE INDEX jobs_expired_lease_idx ON jobs (lease_expires_at)
    WHERE status = 'running';
CREATE UNIQUE INDEX jobs_active_idempotency_idx ON jobs (workspace_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL AND status IN ('queued', 'running', 'retry_wait');
CREATE INDEX jobs_workspace_created_idx ON jobs (workspace_id, created_at DESC, id DESC);
CREATE INDEX jobs_workspace_status_created_idx ON jobs (workspace_id, status, created_at DESC, id DESC);
CREATE INDEX jobs_model_request_idx ON jobs(workspace_id,idempotency_key) WHERE kind='reindex-workspace';

COMMENT ON TABLE jobs IS 'Durable background work claimed and recovered through PostgreSQL leases.';
COMMENT ON COLUMN jobs.id IS 'Stable job identifier.';
COMMENT ON COLUMN jobs.workspace_id IS 'Workspace that owns the job.';
COMMENT ON COLUMN jobs.kind IS 'Worker operation that interprets the job payload.';
COMMENT ON COLUMN jobs.queue IS 'Worker pool allowed to claim the job.';
COMMENT ON COLUMN jobs.args_version IS 'Version of the kind-specific payload contract.';
COMMENT ON COLUMN jobs.payload IS 'Kind-specific job arguments.';
COMMENT ON COLUMN jobs.idempotency_key IS 'Optional key preventing duplicate active work in a workspace.';
COMMENT ON COLUMN jobs.status IS 'Current durable lifecycle state.';
COMMENT ON COLUMN jobs.attempt IS 'Number of attempts already started.';
COMMENT ON COLUMN jobs.max_attempts IS 'Maximum attempts allowed before the job becomes dead.';
COMMENT ON COLUMN jobs.available_at IS 'Earliest time at which a worker may claim the job.';
COMMENT ON COLUMN jobs.lease_owner IS 'Worker identity currently holding the lease.';
COMMENT ON COLUMN jobs.lease_expires_at IS 'Time after which another worker may recover the running job.';
COMMENT ON COLUMN jobs.cancellation_requested IS 'Whether cooperative cancellation was requested for running work.';
COMMENT ON COLUMN jobs.reason IS 'Latest failure, cancellation, or terminal explanation.';
COMMENT ON COLUMN jobs.created_at IS 'Time at which the job was enqueued.';
COMMENT ON COLUMN jobs.started_at IS 'Time at which the first attempt started.';
COMMENT ON COLUMN jobs.finished_at IS 'Time at which the job reached a terminal state.';
COMMENT ON COLUMN jobs.updated_at IS 'Time at which the job record last changed.';
