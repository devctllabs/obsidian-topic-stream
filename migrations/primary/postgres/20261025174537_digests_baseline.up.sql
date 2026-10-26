CREATE TABLE digest_definitions (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name text NOT NULL,
    topics jsonb NOT NULL,
    timezone text NOT NULL,
    recurrence jsonb NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    version integer NOT NULL DEFAULT 1 CONSTRAINT digest_definitions_version_check CHECK (version >= 1),
    next_run_at timestamptz,
    last_scheduled_for timestamptz,
    last_successful_at timestamptz,
    deleted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT digest_definitions_workspace_id_id_key UNIQUE (workspace_id,id)
);

CREATE INDEX digest_definitions_due_idx
    ON digest_definitions (workspace_id, next_run_at)
    WHERE enabled AND deleted_at IS NULL AND next_run_at IS NOT NULL;

COMMENT ON TABLE digest_definitions IS 'Reusable schedules and ordered Topic selections for Digest Issues.';
COMMENT ON COLUMN digest_definitions.id IS 'Stable Digest Definition identifier.';
COMMENT ON COLUMN digest_definitions.workspace_id IS 'Workspace that owns the Digest Definition.';
COMMENT ON COLUMN digest_definitions.name IS 'User-visible Digest Definition name.';
COMMENT ON COLUMN digest_definitions.topics IS 'Ordered Topic identifiers and per-Topic item limits.';
COMMENT ON COLUMN digest_definitions.timezone IS 'IANA timezone used to interpret the recurrence.';
COMMENT ON COLUMN digest_definitions.recurrence IS 'Structured recurrence rule for scheduled generation.';
COMMENT ON COLUMN digest_definitions.enabled IS 'Whether the scheduler may produce new Digest Issues.';
COMMENT ON COLUMN digest_definitions.version IS 'Revision incremented when generation semantics change.';
COMMENT ON COLUMN digest_definitions.next_run_at IS 'Next instant at which the scheduler should enqueue generation.';
COMMENT ON COLUMN digest_definitions.last_scheduled_for IS 'Most recent schedule instant for which generation was enqueued.';
COMMENT ON COLUMN digest_definitions.last_successful_at IS 'Most recent schedule instant that produced a Digest Issue.';
COMMENT ON COLUMN digest_definitions.deleted_at IS 'Soft-deletion time, or null while active.';
COMMENT ON COLUMN digest_definitions.created_at IS 'Time at which the Digest Definition was created.';
COMMENT ON COLUMN digest_definitions.updated_at IS 'Time at which the Digest Definition last changed.';

CREATE TABLE digest_issues (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    digest_id uuid NOT NULL REFERENCES digest_definitions(id) ON DELETE CASCADE,
    title text NOT NULL,
    status text NOT NULL DEFAULT 'complete' CONSTRAINT digest_issues_status_check CHECK (
        status IN ('complete', 'degraded')
    ),
    scheduled_for timestamptz,
    sections jsonb NOT NULL,
    generated_at timestamptz NOT NULL DEFAULT now(),
    generation_job_id uuid NOT NULL,
    CONSTRAINT digest_issues_generation_job_id_key UNIQUE (generation_job_id),
    CONSTRAINT digest_issues_workspace_id_id_key UNIQUE (workspace_id,id),
    CONSTRAINT digest_issues_workspace_job_fk FOREIGN KEY (workspace_id,generation_job_id) REFERENCES jobs(workspace_id,id),
    CONSTRAINT digest_issues_workspace_digest_fk FOREIGN KEY (workspace_id,digest_id) REFERENCES digest_definitions(workspace_id,id) ON DELETE CASCADE
);

CREATE INDEX digest_issues_workspace_generated_idx
    ON digest_issues (workspace_id, generated_at DESC, id DESC);
CREATE INDEX digest_issues_workspace_digest_generated_idx
    ON digest_issues (workspace_id, digest_id, generated_at DESC, id DESC);
CREATE UNIQUE INDEX digest_issues_scheduled_once_idx
    ON digest_issues (digest_id, scheduled_for)
    WHERE scheduled_for IS NOT NULL;

COMMENT ON TABLE digest_issues IS 'Immutable generated results for a Digest Definition at a point in time.';
COMMENT ON COLUMN digest_issues.id IS 'Stable Digest Issue identifier.';
COMMENT ON COLUMN digest_issues.workspace_id IS 'Workspace that owns the Digest Issue.';
COMMENT ON COLUMN digest_issues.digest_id IS 'Digest Definition that produced the issue.';
COMMENT ON COLUMN digest_issues.title IS 'Rendered user-visible issue title.';
COMMENT ON COLUMN digest_issues.status IS 'Whether all Topic sections were generated successfully.';
COMMENT ON COLUMN digest_issues.scheduled_for IS 'Schedule instant represented by this issue, when scheduled.';
COMMENT ON COLUMN digest_issues.sections IS 'Immutable ordered Topic sections and selected Items.';
COMMENT ON COLUMN digest_issues.generated_at IS 'Time at which generation completed.';

CREATE TABLE digest_consumed_items (
    digest_id uuid NOT NULL REFERENCES digest_definitions(id) ON DELETE CASCADE,
    item_id uuid NOT NULL,
    PRIMARY KEY (digest_id, item_id)
);

COMMENT ON TABLE digest_consumed_items IS 'Item consumption set maintained atomically with immutable Digest Issues.';
COMMENT ON COLUMN digest_consumed_items.digest_id IS 'Digest Definition that has consumed the Item.';
COMMENT ON COLUMN digest_consumed_items.item_id IS 'Historical Item identifier, retained independently of canonical Item lifetime.';

CREATE TABLE digest_build_requests (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    digest_id uuid NOT NULL,
    request_kind text NOT NULL CHECK(request_kind IN ('manual','retry')),
    identity_id uuid NOT NULL,
    job_id uuid NOT NULL,
    created_at timestamptz NOT NULL,
    PRIMARY KEY(workspace_id,digest_id,request_kind,identity_id),
    FOREIGN KEY(workspace_id,digest_id) REFERENCES digest_definitions(workspace_id,id) ON DELETE CASCADE,
    FOREIGN KEY(workspace_id,job_id) REFERENCES jobs(workspace_id,id)
);

COMMENT ON TABLE digest_build_requests IS 'Permanent manual request and Retry source identities with terminal Job references have no TTL.';
COMMENT ON COLUMN digest_build_requests.workspace_id IS 'Workspace owning the request, Definition and Job.';
COMMENT ON COLUMN digest_build_requests.digest_id IS 'Definition locked during admission, including soft-deleted replays.';
COMMENT ON COLUMN digest_build_requests.request_kind IS 'Separate namespaces for manual request UUIDs and Retry source Job UUIDs.';
COMMENT ON COLUMN digest_build_requests.identity_id IS 'Caller request UUID or source Job UUID, according to request_kind.';
COMMENT ON COLUMN digest_build_requests.job_id IS 'Original admitted or coalesced Job, retained after terminal completion.';
COMMENT ON COLUMN digest_build_requests.created_at IS 'Time the request binding was admitted.';

COMMENT ON COLUMN digest_issues.generation_job_id IS 'Generating Job identity with one immutable result retained across acknowledgement loss and recovery.';
