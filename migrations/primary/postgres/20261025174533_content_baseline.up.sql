CREATE TABLE sources (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name text NOT NULL,
    config jsonb NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    poll_interval_minutes integer NOT NULL
        CONSTRAINT sources_poll_interval_minutes_check CHECK (poll_interval_minutes BETWEEN 15 AND 10080),
    last_synced_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT sources_workspace_config_key UNIQUE (workspace_id, config)
);

COMMENT ON TABLE sources IS 'Configured upstream streams that can be polled for entries.';
COMMENT ON COLUMN sources.id IS 'Stable identifier of the Source.';
COMMENT ON COLUMN sources.workspace_id IS 'Workspace that owns the Source.';
COMMENT ON COLUMN sources.name IS 'User-visible Source name.';
COMMENT ON COLUMN sources.config IS 'Typed Source configuration encoded as JSON.';
COMMENT ON COLUMN sources.enabled IS 'Whether scheduled polling is enabled for the Source.';
COMMENT ON COLUMN sources.poll_interval_minutes IS 'Minimum interval in minutes between scheduled polls.';
COMMENT ON COLUMN sources.last_synced_at IS 'Time when the Source most recently completed synchronization.';
COMMENT ON COLUMN sources.created_at IS 'Time when the Source was created.';
COMMENT ON COLUMN sources.updated_at IS 'Time when the Source configuration was last changed.';

CREATE TABLE items (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    kind text NOT NULL DEFAULT 'article'
        CONSTRAINT items_kind_check CHECK (kind IN ('article', 'paper', 'video', 'repository')),
    details jsonb NOT NULL,
    title text NOT NULL,
    url text NOT NULL,
    canonical_url text NOT NULL,
    excerpt text NOT NULL DEFAULT '',
    content_markdown text NOT NULL DEFAULT '',
    extraction_version text NOT NULL DEFAULT 'legacy-v1',
    authors jsonb NOT NULL DEFAULT '[]'::jsonb,
    published_at timestamptz,
    discovered_at timestamptz NOT NULL DEFAULT now(),
    content_origin text NOT NULL DEFAULT 'unknown'
    CONSTRAINT items_content_origin_check CHECK(content_origin IN (
        'unknown','article_body','article_description','paper_abstract','video_description',
        'repository_readme','repository_description','empty'
    )),
    CONSTRAINT items_workspace_canonical_url_key UNIQUE (workspace_id, canonical_url),
    CONSTRAINT items_workspace_id_id_key UNIQUE (workspace_id, id)
);

COMMENT ON TABLE items IS 'Canonical content observed through one or more Source Entries.';
COMMENT ON COLUMN items.id IS 'Stable identifier of the canonical Item.';
COMMENT ON COLUMN items.workspace_id IS 'Workspace that owns the Item.';
COMMENT ON COLUMN items.kind IS 'Typed Item kind: article, paper, video, or repository.';
COMMENT ON COLUMN items.details IS 'Kind-specific Item details encoded as a discriminated JSON union.';
COMMENT ON COLUMN items.title IS 'Normalized display title of the Item.';
COMMENT ON COLUMN items.url IS 'Original content URL selected for the canonical Item.';
COMMENT ON COLUMN items.canonical_url IS 'Normalized URL used for Workspace-level exact deduplication.';
COMMENT ON COLUMN items.excerpt IS 'Compact Markdown excerpt used by list views.';
COMMENT ON COLUMN items.content_markdown IS 'Canonical cleaned Markdown owned by the backend.';
COMMENT ON COLUMN items.extraction_version IS 'Version of the pipeline that produced canonical content.';
COMMENT ON COLUMN items.authors IS 'Ordered author names encoded as a JSON array.';
COMMENT ON COLUMN items.published_at IS 'Upstream publication time when available.';
COMMENT ON COLUMN items.discovered_at IS 'Time when the Item was first discovered in the Workspace.';

CREATE INDEX items_content_date_idx ON items (workspace_id, COALESCE(published_at, discovered_at) DESC, id DESC);

CREATE TABLE source_entries (
    source_id uuid NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
    external_id text NOT NULL,
    item_id uuid NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    url text NOT NULL,
    details jsonb NOT NULL,
    extraction_version text NOT NULL DEFAULT 'legacy-v1',
    published_at timestamptz,
    discovered_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (source_id, external_id)
);

COMMENT ON TABLE source_entries IS 'Upstream occurrences preserved for canonical Items.';
COMMENT ON COLUMN source_entries.source_id IS 'Source in which the upstream occurrence was observed.';
COMMENT ON COLUMN source_entries.external_id IS 'Source-defined stable identifier of the occurrence.';
COMMENT ON COLUMN source_entries.item_id IS 'Canonical Item represented by this occurrence.';
COMMENT ON COLUMN source_entries.url IS 'Original URL supplied by the Source Entry.';
COMMENT ON COLUMN source_entries.details IS 'Compact kind-specific Source Entry details encoded as JSON.';
COMMENT ON COLUMN source_entries.extraction_version IS 'Version of the pipeline that processed this occurrence.';
COMMENT ON COLUMN source_entries.published_at IS 'Publication time reported by the upstream Source.';
COMMENT ON COLUMN source_entries.discovered_at IS 'Time when this occurrence was first discovered.';
COMMENT ON COLUMN source_entries.updated_at IS 'Time when stored details for this occurrence last changed.';

CREATE INDEX source_entries_item_idx ON source_entries (item_id, source_id);

CREATE TABLE item_feedback (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    item_id uuid NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    sentiment text NOT NULL
        CONSTRAINT item_feedback_sentiment_check CHECK (sentiment IN ('like', 'dislike')),
    comment text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, item_id)
);

COMMENT ON TABLE item_feedback IS 'Current Personal Feedback for canonical Items.';
COMMENT ON COLUMN item_feedback.workspace_id IS 'Workspace in which the feedback applies.';
COMMENT ON COLUMN item_feedback.item_id IS 'Item receiving the current feedback.';
COMMENT ON COLUMN item_feedback.sentiment IS 'Current Like or Dislike decision.';
COMMENT ON COLUMN item_feedback.comment IS 'Optional user explanation for the decision.';
COMMENT ON COLUMN item_feedback.created_at IS 'Time when feedback was first recorded for the Item.';
COMMENT ON COLUMN item_feedback.updated_at IS 'Time when the current feedback was last changed.';

CREATE TABLE pending_item_feedback (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    canonical_url text NOT NULL,
    sentiment text NOT NULL
        CONSTRAINT pending_item_feedback_sentiment_check CHECK (sentiment IN ('like', 'dislike')),
    comment text,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (workspace_id, canonical_url)
);

COMMENT ON TABLE pending_item_feedback IS 'Imported Personal Feedback waiting for its Item to be ingested.';
COMMENT ON COLUMN pending_item_feedback.workspace_id IS 'Workspace into which the feedback was imported.';
COMMENT ON COLUMN pending_item_feedback.canonical_url IS 'Canonical Item URL used to attach feedback after ingestion.';
COMMENT ON COLUMN pending_item_feedback.sentiment IS 'Imported Like or Dislike decision.';
COMMENT ON COLUMN pending_item_feedback.comment IS 'Optional imported explanation for the decision.';
COMMENT ON COLUMN pending_item_feedback.created_at IS 'Original time when the feedback was first recorded.';
COMMENT ON COLUMN pending_item_feedback.updated_at IS 'Original time when the imported feedback was last changed.';

CREATE TABLE item_state (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    item_id uuid NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    reading_state text NOT NULL DEFAULT 'unread'
        CONSTRAINT item_state_reading_state_check CHECK (reading_state IN ('unread', 'read')),
    visibility text NOT NULL DEFAULT 'visible'
        CONSTRAINT item_state_visibility_check CHECK (visibility IN ('visible', 'hidden')),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, item_id)
);

COMMENT ON TABLE item_state IS 'Current Reading State and Item Visibility overrides.';
COMMENT ON COLUMN item_state.workspace_id IS 'Workspace in which the state applies.';
COMMENT ON COLUMN item_state.item_id IS 'Item whose state is overridden.';
COMMENT ON COLUMN item_state.reading_state IS 'Current deliberate read or unread state.';
COMMENT ON COLUMN item_state.visibility IS 'Current visible or hidden state independent of preference.';
COMMENT ON COLUMN item_state.updated_at IS 'Time when either state value last changed.';

COMMENT ON COLUMN items.content_origin IS 'Explicit source evidence for full article or README versus descriptive fallback with unknown as the safe default.';
