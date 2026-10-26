CREATE TABLE feed_state (
    workspace_id uuid PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
    head_ordinal bigint NOT NULL DEFAULT 0 CHECK (head_ordinal >= 0)
);
CREATE TABLE feed_preparation_requests (
    workspace_id uuid PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
    first_dirty_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX feed_preparation_requests_due_idx ON feed_preparation_requests(first_dirty_at,workspace_id);
COMMENT ON TABLE feed_preparation_requests IS 'Coalesced serving projection refill requests, drained under the shared Feed head lock.';
CREATE TABLE feed_publications (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    item_id uuid NOT NULL,
    batch_id uuid NOT NULL,
    published_at timestamptz NOT NULL,
    ordinal bigint NOT NULL CHECK (ordinal > 0),
    matched_topic_ids uuid[] NOT NULL DEFAULT '{}',
    topic_inputs jsonb NOT NULL DEFAULT '{}',
    score double precision NOT NULL,
    signals jsonb NOT NULL DEFAULT '{}',
    pipeline text NOT NULL,
    model_id uuid NOT NULL,
    PRIMARY KEY (workspace_id,item_id),
    UNIQUE (workspace_id,ordinal),
    FOREIGN KEY (workspace_id,item_id) REFERENCES items(workspace_id,id) ON DELETE CASCADE
);
COMMENT ON TABLE feed_publications IS 'Lifetime-unique Workspace publication facts with no display TTL.';

-- Synchronous, rebuildable read access path. Immutable publication facts remain in the ledger.
CREATE TABLE feed_display_entries (
    workspace_id uuid NOT NULL,
    item_id uuid NOT NULL,
    ordinal bigint NOT NULL,
    topic_ids uuid[] NOT NULL,
    PRIMARY KEY (workspace_id,item_id),
    FOREIGN KEY (workspace_id,item_id) REFERENCES feed_publications(workspace_id,item_id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX feed_display_ordinal_idx ON feed_display_entries(workspace_id,ordinal DESC);
CREATE INDEX feed_display_topics_idx ON feed_display_entries USING gin(topic_ids);
ALTER TABLE feed_display_entries ALTER COLUMN topic_ids SET STATISTICS 1000;
COMMENT ON TABLE feed_display_entries IS 'Current visible, All-eligible publication keys maintained atomically with canonical eligibility writes under the Feed head lock.';
COMMENT ON COLUMN feed_display_entries.topic_ids IS 'Original plus Add minus Wrong identifiers. Deleted original identifiers may remain and are checked against current Topics on reads.';

CREATE TABLE topic_candidate_state (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    topic_id uuid NOT NULL,
    requested_version bigint NOT NULL DEFAULT 1,
    ready_version bigint NOT NULL DEFAULT 0,
    input_version bigint NOT NULL DEFAULT 1,
    prepared_input_version bigint NOT NULL DEFAULT 0,
    first_dirty_at timestamptz,
    prepared_at timestamptz,
    next_eligible_at timestamptz,
    inputs jsonb NOT NULL DEFAULT '{}',
    include_embedding double precision[],
    exclude_embedding double precision[],
    PRIMARY KEY (workspace_id,topic_id),
    FOREIGN KEY (workspace_id,topic_id) REFERENCES topics(workspace_id,id) ON DELETE CASCADE
);
CREATE INDEX topic_candidate_dirty_idx ON topic_candidate_state (first_dirty_at,workspace_id)
    WHERE requested_version > ready_version;
CREATE INDEX topic_candidate_boundary_idx ON topic_candidate_state (next_eligible_at,workspace_id)
    WHERE next_eligible_at IS NOT NULL;
CREATE TABLE topic_candidates (
    workspace_id uuid NOT NULL,
    topic_id uuid NOT NULL,
    item_id uuid NOT NULL,
    position integer NOT NULL CHECK (position BETWEEN 1 AND 100),
    score double precision NOT NULL,
    signals jsonb NOT NULL DEFAULT '{}',
    PRIMARY KEY (workspace_id,topic_id,item_id),
    UNIQUE (workspace_id,topic_id,position),
    FOREIGN KEY (workspace_id,topic_id) REFERENCES topic_candidate_state(workspace_id,topic_id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id,item_id) REFERENCES items(workspace_id,id) ON DELETE CASCADE
);
COMMENT ON TABLE topic_candidates IS 'Replaceable bounded Topic lists referencing canonical Items, not an accumulation log.';

CREATE INDEX topic_candidates_item_idx ON topic_candidates(workspace_id,item_id);
