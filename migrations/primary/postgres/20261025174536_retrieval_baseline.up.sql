CREATE TABLE embedding_model_collections (
    embedding_model_id uuid PRIMARY KEY REFERENCES embedding_models(id) ON DELETE CASCADE,
    schema_version integer NOT NULL CONSTRAINT embedding_model_collections_schema_check CHECK (schema_version >= 1),
    collection_name text NOT NULL UNIQUE,
    incarnation uuid,
    status text NOT NULL CONSTRAINT embedding_model_collections_status_check CHECK (
        status IN ('initializing', 'ready', 'failed')
    ),
    failure_reason text,
    updated_at timestamptz NOT NULL
);

COMMENT ON TABLE embedding_model_collections IS 'One shared rebuildable Qdrant collection for each Embedding Model schema.';
COMMENT ON COLUMN embedding_model_collections.embedding_model_id IS 'Embedding Model whose immutable dimensions define the collection.';
COMMENT ON COLUMN embedding_model_collections.schema_version IS 'Version of the Qdrant vector, sparse, payload, and index layout.';
COMMENT ON COLUMN embedding_model_collections.collection_name IS 'Deterministic Qdrant collection name.';
COMMENT ON COLUMN embedding_model_collections.status IS 'Latest collection initialization result.';
COMMENT ON COLUMN embedding_model_collections.failure_reason IS 'Sanitized schema or provider failure safe to expose.';
COMMENT ON COLUMN embedding_model_collections.updated_at IS 'Time when initialization state last changed.';

CREATE TABLE workspace_retrieval_state (
    workspace_id uuid PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
    serving_model_id uuid REFERENCES embedding_models(id),
    serving_incarnation uuid,
    transition_model_id uuid REFERENCES embedding_models(id),
    transition_job_id uuid REFERENCES jobs(id),
    selection_generation bigint NOT NULL DEFAULT 0 CHECK (selection_generation >= 0),
    transition_lease_owner text NOT NULL DEFAULT '',
    reconcile_requested boolean NOT NULL DEFAULT false,
    maintenance_requested boolean NOT NULL DEFAULT false,
    updated_at timestamptz NOT NULL,
    latest_transition_job_id uuid,
    CONSTRAINT workspace_latest_transition_job_fkey
        FOREIGN KEY (workspace_id,latest_transition_job_id) REFERENCES jobs(workspace_id,id)
);

CREATE INDEX workspace_retrieval_maintenance_idx ON workspace_retrieval_state(workspace_id) WHERE maintenance_requested;

COMMENT ON TABLE workspace_retrieval_state IS 'Serving and in-progress Embedding Model state for each Workspace.';
COMMENT ON COLUMN workspace_retrieval_state.workspace_id IS 'Workspace whose retrieval pointer is recorded.';
COMMENT ON COLUMN workspace_retrieval_state.serving_model_id IS 'Ready model currently answering retrieval, or null before initial build.';
COMMENT ON COLUMN workspace_retrieval_state.transition_model_id IS 'Target model currently receiving a Workspace rebuild.';
COMMENT ON COLUMN workspace_retrieval_state.transition_job_id IS 'Newest durable Job representing the current transition operation.';
COMMENT ON COLUMN workspace_retrieval_state.updated_at IS 'Time when transition or serving state last changed.';

CREATE TABLE item_projection_state (
    workspace_id uuid NOT NULL,
    item_id uuid NOT NULL,
    revision uuid NOT NULL,
    updated_at timestamptz NOT NULL,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    PRIMARY KEY (workspace_id, item_id),
    CONSTRAINT item_projection_state_item_scope_fkey
        FOREIGN KEY (workspace_id,item_id) REFERENCES items(workspace_id,id) ON DELETE CASCADE
) WITH (fillfactor=90);

CREATE TABLE workspace_model_progress (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    embedding_model_id uuid NOT NULL REFERENCES embedding_models(id) ON DELETE CASCADE,
    generation uuid NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
    collection_incarnation uuid,
    retained boolean NOT NULL DEFAULT true,
    seed_after uuid,
    seed_complete boolean NOT NULL DEFAULT false,
    selected_at timestamptz NOT NULL,
    selection_order bigint NOT NULL DEFAULT 0,
    PRIMARY KEY (workspace_id, embedding_model_id)
);
COMMENT ON TABLE workspace_model_progress IS 'Resumable initialization retained for the Serving Model and two recently selected models. Generation changes fence evicted synchronization state.';
CREATE INDEX workspace_model_progress_model_idx ON workspace_model_progress(embedding_model_id,workspace_id) WHERE retained;

CREATE TABLE retrieval_rebuilds (
    job_id uuid PRIMARY KEY REFERENCES jobs(id) ON DELETE CASCADE,
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    embedding_model_id uuid NOT NULL REFERENCES embedding_models(id),
    model_generation uuid NOT NULL,
    active boolean NOT NULL DEFAULT true,
    superseded boolean NOT NULL DEFAULT false
);
CREATE UNIQUE INDEX retrieval_rebuilds_current_idx ON retrieval_rebuilds(workspace_id) WHERE NOT superseded;
COMMENT ON TABLE retrieval_rebuilds IS 'Execution ownership of model synchronization. Initialization progress belongs to Workspace and model.';

COMMENT ON TABLE item_projection_state IS 'Current opaque retrieval projection revision for each Item.';
COMMENT ON COLUMN item_projection_state.workspace_id IS 'Workspace that owns the Item projection.';
COMMENT ON COLUMN item_projection_state.item_id IS 'Canonical Item whose projection changed.';
COMMENT ON COLUMN item_projection_state.revision IS 'Opaque UUIDv7 equality token. Ordering comparisons are forbidden.';
COMMENT ON COLUMN item_projection_state.updated_at IS 'Time when canonical projection inputs last changed.';

CREATE TABLE retrieval_projection_pending (
    workspace_id uuid NOT NULL,
    item_id uuid NOT NULL,
    embedding_model_id uuid NOT NULL REFERENCES embedding_models(id) ON DELETE CASCADE,
    revision uuid NOT NULL,
    model_generation uuid NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (workspace_id, item_id, embedding_model_id),
    CONSTRAINT retrieval_projection_pending_state_fkey
        FOREIGN KEY (workspace_id,item_id) REFERENCES item_projection_state(workspace_id,item_id) ON DELETE CASCADE
) WITH (fillfactor=90);

CREATE INDEX retrieval_projection_pending_model_idx
    ON retrieval_projection_pending (workspace_id, embedding_model_id, item_id);

COMMENT ON TABLE retrieval_projection_pending IS 'Latest desired delivery per Workspace, Item, and Embedding Model. Newer revisions replace older work.';
COMMENT ON COLUMN retrieval_projection_pending.workspace_id IS 'Workspace owning the canonical Item.';
COMMENT ON COLUMN retrieval_projection_pending.item_id IS 'Item whose current snapshot still needs delivery.';
COMMENT ON COLUMN retrieval_projection_pending.embedding_model_id IS 'Serving or transition destination that missed delivery.';
COMMENT ON COLUMN retrieval_projection_pending.revision IS 'Exact current projection revision that must be delivered.';
COMMENT ON COLUMN retrieval_projection_pending.updated_at IS 'Time when this latest desired delivery was recorded.';

COMMENT ON CONSTRAINT retrieval_projection_pending_state_fkey ON retrieval_projection_pending IS
    'Pending work belongs to an existing scoped projection state. This transitively enforces Workspace and Item ownership without repeatedly locking canonical content during capture.';

CREATE TABLE retrieval_rebuild_progress (
    job_id uuid PRIMARY KEY REFERENCES retrieval_rebuilds(job_id) ON DELETE CASCADE,
    phase text NOT NULL CHECK (phase IN ('preparing','indexing','catching_up','activating','completed')),
    total_items bigint CHECK (total_items >= 0),
    processed_items bigint NOT NULL DEFAULT 0 CHECK (processed_items >= 0),
    updated_at timestamptz NOT NULL,
    CHECK (total_items IS NULL OR processed_items <= total_items)
);
CREATE TABLE retrieval_rebuild_cohort (
    job_id uuid NOT NULL REFERENCES retrieval_rebuild_progress(job_id) ON DELETE CASCADE,
    item_id uuid NOT NULL,
    PRIMARY KEY (job_id,item_id)
);
COMMENT ON TABLE retrieval_rebuild_progress IS 'Fixed initial workload counters. Completed cohort work is distinct from successful catch-up and cutover.';
COMMENT ON TABLE retrieval_rebuild_cohort IS 'Remaining members of the captured initial Item set. Only an accepted target ACK deletes a member and atomically increments its counter.';

CREATE TABLE model_transition_jobs (
    job_id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    embedding_model_id uuid NOT NULL REFERENCES embedding_models(id),
    selection_generation bigint NOT NULL CHECK (selection_generation >= 0),
    FOREIGN KEY (workspace_id, job_id) REFERENCES jobs(workspace_id,id) ON DELETE CASCADE
);
CREATE INDEX model_transition_jobs_selection_idx ON model_transition_jobs(workspace_id,selection_generation);
COMMENT ON TABLE model_transition_jobs IS 'Verified public model-selection identity for admitted rebuild Jobs. Private arguments are not the public contract.';

COMMENT ON COLUMN workspace_retrieval_state.latest_transition_job_id IS 'Latest admitted operation for this selection, retained across working-pointer cleanup.';
