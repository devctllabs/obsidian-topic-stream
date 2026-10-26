CREATE TABLE topics (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name text NOT NULL,
    include_text text NOT NULL,
    exclude_text text NOT NULL DEFAULT '',
    mode text NOT NULL DEFAULT 'ordinary'
        CONSTRAINT topics_mode_check CHECK (mode IN ('ordinary', 'strict')),
    enabled boolean NOT NULL DEFAULT true,
    revision integer NOT NULL DEFAULT 1,
    correction_epoch bigint NOT NULL DEFAULT 1
        CONSTRAINT topics_correction_epoch_check CHECK (correction_epoch >= 1),
    correction_version bigint NOT NULL DEFAULT 0
        CONSTRAINT topics_correction_version_check CHECK (correction_version >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT topics_workspace_name_key UNIQUE (workspace_id, name),
    CONSTRAINT topics_workspace_id_id_key UNIQUE (workspace_id, id)
);

COMMENT ON TABLE topics IS 'Saved thematic subscriptions with natural-language criteria and scopes.';
COMMENT ON COLUMN topics.id IS 'Stable identifier of the Topic.';
COMMENT ON COLUMN topics.workspace_id IS 'Workspace that owns the Topic.';
COMMENT ON COLUMN topics.name IS 'User-visible Topic name.';
COMMENT ON COLUMN topics.include_text IS 'Natural-language criteria describing content to include.';
COMMENT ON COLUMN topics.exclude_text IS 'Natural-language criteria describing content to exclude.';
COMMENT ON COLUMN topics.mode IS 'Ordinary or strict semantic membership mode.';
COMMENT ON COLUMN topics.enabled IS 'Whether the Topic participates in current Feed and Digest operations.';
COMMENT ON COLUMN topics.revision IS 'Current immutable semantic revision number.';
COMMENT ON COLUMN topics.correction_epoch IS 'Generation of the current Topic Membership Correction set, incremented only by clear-all.';
COMMENT ON COLUMN topics.correction_version IS 'Monotonic version incremented by every effective Topic Membership Correction change.';
COMMENT ON COLUMN topics.created_at IS 'Time when the Topic was created.';
COMMENT ON COLUMN topics.updated_at IS 'Time when the Topic was last changed.';

CREATE TABLE topic_sources (
    topic_id uuid NOT NULL REFERENCES topics(id) ON DELETE CASCADE,
    source_id uuid NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
    PRIMARY KEY (topic_id, source_id)
);

COMMENT ON TABLE topic_sources IS 'Optional Source scope of each Topic.';
COMMENT ON COLUMN topic_sources.topic_id IS 'Topic whose Source scope is constrained.';
COMMENT ON COLUMN topic_sources.source_id IS 'Source included in the Topic scope.';

CREATE TABLE topic_item_kinds (
    topic_id uuid NOT NULL REFERENCES topics(id) ON DELETE CASCADE,
    item_kind text NOT NULL
        CONSTRAINT topic_item_kinds_kind_check CHECK (item_kind IN ('article', 'paper', 'video', 'repository')),
    PRIMARY KEY (topic_id, item_kind)
);

COMMENT ON TABLE topic_item_kinds IS 'Optional Item-kind scope of each Topic.';
COMMENT ON COLUMN topic_item_kinds.topic_id IS 'Topic whose Item kinds are constrained.';
COMMENT ON COLUMN topic_item_kinds.item_kind IS 'Item kind included in the Topic scope.';

CREATE TABLE topic_revisions (
    topic_id uuid NOT NULL REFERENCES topics(id) ON DELETE CASCADE,
    revision integer NOT NULL CONSTRAINT topic_revisions_revision_check CHECK (revision >= 1),
    source_ids uuid[] NOT NULL,
    item_kinds text[] NOT NULL,
    include_text text NOT NULL,
    exclude_text text NOT NULL,
    mode text NOT NULL CONSTRAINT topic_revisions_mode_check CHECK (mode IN ('ordinary', 'strict')),
    created_at timestamptz NOT NULL,
    PRIMARY KEY (topic_id, revision)
);

COMMENT ON TABLE topic_revisions IS 'Immutable semantic history of Topic configuration.';
COMMENT ON COLUMN topic_revisions.topic_id IS 'Topic that owns the immutable revision.';
COMMENT ON COLUMN topic_revisions.revision IS 'Semantic revision number within the Topic.';
COMMENT ON COLUMN topic_revisions.source_ids IS 'Source scope captured by the revision.';
COMMENT ON COLUMN topic_revisions.item_kinds IS 'Item-kind scope captured by the revision.';
COMMENT ON COLUMN topic_revisions.include_text IS 'Include criteria captured by the revision.';
COMMENT ON COLUMN topic_revisions.exclude_text IS 'Exclude criteria captured by the revision.';
COMMENT ON COLUMN topic_revisions.mode IS 'Membership mode captured by the revision.';
COMMENT ON COLUMN topic_revisions.created_at IS 'Time when the immutable revision was created.';

CREATE TABLE topic_corrections (
    topic_id uuid NOT NULL REFERENCES topics(id) ON DELETE CASCADE,
    item_id uuid NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    membership text NOT NULL
        CONSTRAINT topic_corrections_membership_check CHECK (membership IN ('include', 'exclude')),
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (topic_id, item_id)
);

CREATE INDEX topic_corrections_item_idx ON topic_corrections (item_id, topic_id);

COMMENT ON TABLE topic_corrections IS 'Current explicit Topic Membership Corrections for Items.';
COMMENT ON COLUMN topic_corrections.item_id IS 'Item whose automatic Topic membership is overridden.';
COMMENT ON COLUMN topic_corrections.topic_id IS 'Topic receiving the membership correction.';
COMMENT ON COLUMN topic_corrections.membership IS 'Explicitly included or explicitly excluded membership.';
COMMENT ON COLUMN topic_corrections.updated_at IS 'Time when the current correction last changed.';

CREATE TABLE topic_gate_calibrations (
    topic_id uuid NOT NULL REFERENCES topics(id) ON DELETE CASCADE,
    embedding_model_id uuid NOT NULL REFERENCES embedding_models(id) ON DELETE CASCADE,
    topic_revision integer NOT NULL CONSTRAINT topic_gate_calibrations_topic_revision_check CHECK (topic_revision >= 1),
    correction_epoch bigint NOT NULL CONSTRAINT topic_gate_calibrations_correction_epoch_check CHECK (correction_epoch >= 1),
    correction_version bigint NOT NULL CONSTRAINT topic_gate_calibrations_correction_version_check CHECK (correction_version >= 0),
    algorithm_version text NOT NULL,
    include_threshold double precision NOT NULL CONSTRAINT topic_gate_calibrations_include_threshold_check CHECK (include_threshold BETWEEN -1 AND 1),
    exclude_threshold double precision NOT NULL CONSTRAINT topic_gate_calibrations_exclude_threshold_check CHECK (exclude_threshold BETWEEN -1 AND 1),
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (topic_id, embedding_model_id)
);

COMMENT ON TABLE topic_gate_calibrations IS 'Latest materialized Topic gate thresholds for each Embedding Model.';
COMMENT ON COLUMN topic_gate_calibrations.topic_id IS 'Topic whose semantic gate was calibrated.';
COMMENT ON COLUMN topic_gate_calibrations.embedding_model_id IS 'Embedding Model whose vector space produced the thresholds.';
COMMENT ON COLUMN topic_gate_calibrations.topic_revision IS 'Topic semantic revision used by the calculation.';
COMMENT ON COLUMN topic_gate_calibrations.correction_epoch IS 'Correction-set generation used by the calculation.';
COMMENT ON COLUMN topic_gate_calibrations.correction_version IS 'Latest correction version included by the calculation.';
COMMENT ON COLUMN topic_gate_calibrations.algorithm_version IS 'Version of the calibration algorithm and bootstrap constants.';
COMMENT ON COLUMN topic_gate_calibrations.include_threshold IS 'Effective Include cosine threshold after learning and shrinkage, or bootstrap fallback.';
COMMENT ON COLUMN topic_gate_calibrations.exclude_threshold IS 'Effective Exclude cosine rejection threshold after learning and shrinkage, or bootstrap fallback.';
COMMENT ON COLUMN topic_gate_calibrations.updated_at IS 'Time when this materialization was written.';
