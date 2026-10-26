CREATE TABLE item_summary_inputs (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    item_id uuid NOT NULL,
    source_version bytea NOT NULL CHECK(octet_length(source_version)=32),
    title text NOT NULL,
    markdown text CHECK(markdown IS NULL OR length(btrim(markdown))>0),
    content_origin text NOT NULL CHECK(content_origin IN ('article_body','repository_readme')),
    current_job_id uuid,
    ready_summary_id uuid,
    created_at timestamptz NOT NULL,
    PRIMARY KEY(workspace_id,item_id,source_version),
    CHECK(ready_summary_id IS NULL OR current_job_id IS NOT NULL),
    FOREIGN KEY(workspace_id,current_job_id) REFERENCES jobs(workspace_id,id)
);

CREATE TABLE item_summaries (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    item_id uuid NOT NULL,
    source_version bytea NOT NULL CHECK(octet_length(source_version)=32),
    configuration_fingerprint bytea NOT NULL CHECK(octet_length(configuration_fingerprint)=32),
    configuration jsonb NOT NULL,
    text text NOT NULL CHECK(length(btrim(text)) BETWEEN 1 AND 4000),
    generation_job_id uuid NOT NULL UNIQUE,
    reused_from uuid REFERENCES item_summaries(id),
    created_at timestamptz NOT NULL,
    UNIQUE(workspace_id,item_id,source_version,id),
    FOREIGN KEY(workspace_id,item_id,source_version) REFERENCES item_summary_inputs(workspace_id,item_id,source_version) ON DELETE CASCADE,
    FOREIGN KEY(workspace_id,generation_job_id) REFERENCES jobs(workspace_id,id),
    CHECK(length(text)<=(configuration->>'maxSummaryCharacters')::integer)
);

CREATE INDEX item_summaries_cache_idx ON item_summaries
    (workspace_id,item_id,source_version,configuration_fingerprint,created_at DESC,id DESC);

ALTER TABLE item_summary_inputs ADD CONSTRAINT item_summary_inputs_ready_fk
    FOREIGN KEY(workspace_id,item_id,source_version,ready_summary_id)
    REFERENCES item_summaries(workspace_id,item_id,source_version,id);

CREATE TABLE digest_item_summaries (
    workspace_id uuid NOT NULL,
    issue_id uuid NOT NULL,
    item_id uuid NOT NULL,
    source_version bytea NOT NULL CHECK(octet_length(source_version)=32),
    job_id uuid,
    summary_id uuid,
    requested_at timestamptz NOT NULL,
    PRIMARY KEY(issue_id,item_id),
    CHECK(summary_id IS NULL OR job_id IS NOT NULL),
    FOREIGN KEY(workspace_id,issue_id) REFERENCES digest_issues(workspace_id,id) ON DELETE CASCADE,
    FOREIGN KEY(workspace_id,item_id,source_version) REFERENCES item_summary_inputs(workspace_id,item_id,source_version) ON DELETE CASCADE,
    FOREIGN KEY(workspace_id,job_id) REFERENCES jobs(workspace_id,id),
    FOREIGN KEY(workspace_id,item_id,source_version,summary_id) REFERENCES item_summaries(workspace_id,item_id,source_version,id)
);

CREATE INDEX digest_item_summaries_missing_idx ON digest_item_summaries(workspace_id,item_id,source_version)
    WHERE summary_id IS NULL;

CREATE TABLE item_summary_retries (
    workspace_id uuid NOT NULL,
    source_job_id uuid PRIMARY KEY,
    successor_job_id uuid NOT NULL,
    created_at timestamptz NOT NULL,
    FOREIGN KEY(workspace_id,source_job_id) REFERENCES jobs(workspace_id,id),
    FOREIGN KEY(workspace_id,successor_job_id) REFERENCES jobs(workspace_id,id)
);

COMMENT ON TABLE item_summary_inputs IS 'Exact owned input versions and shared work pointers with Markdown captured only by explicit Summary requests.';
COMMENT ON COLUMN item_summary_inputs.workspace_id IS 'Workspace owning the canonical or retained captured Item.';
COMMENT ON COLUMN item_summary_inputs.item_id IS 'Stable Item identity that remains valid for a retained Digest snapshot.';
COMMENT ON COLUMN item_summary_inputs.source_version IS 'SHA-256 of actual title, text and provenance.';
COMMENT ON COLUMN item_summary_inputs.title IS 'Captured title used in model input.';
COMMENT ON COLUMN item_summary_inputs.markdown IS 'Exact requested article body or README with null for an unrequested Digest marker.';
COMMENT ON COLUMN item_summary_inputs.content_origin IS 'Positive full-content evidence, never a description or abstract.';
COMMENT ON COLUMN item_summary_inputs.current_job_id IS 'Most recently accepted shared work for this input version.';
COMMENT ON COLUMN item_summary_inputs.ready_summary_id IS 'Last ready Item annotation kept readable during later explicit work.';
COMMENT ON COLUMN item_summary_inputs.created_at IS 'Time this exact input version was first recorded.';
COMMENT ON TABLE item_summaries IS 'Immutable generated annotations and actual non-secret Summary configuration with one result per Job.';
COMMENT ON COLUMN item_summaries.id IS 'Stable AI annotation identity.';
COMMENT ON COLUMN item_summaries.workspace_id IS 'Workspace owning this annotation.';
COMMENT ON COLUMN item_summaries.item_id IS 'Item whose actual input was summarized.';
COMMENT ON COLUMN item_summaries.source_version IS 'Exact input version shared with context bindings.';
COMMENT ON COLUMN item_summaries.configuration_fingerprint IS 'SHA-256 of relevant model, prompt, language, limit and algorithm semantics.';
COMMENT ON COLUMN item_summaries.configuration IS 'Actual attempt-start non-secret configuration and version provenance.';
COMMENT ON COLUMN item_summaries.text IS 'Validated plain-text AI output distinct from ordinary excerpts.';
COMMENT ON COLUMN item_summaries.generation_job_id IS 'Actual generating or cache-reusing Job for acknowledgement recovery.';
COMMENT ON COLUMN item_summaries.reused_from IS 'Original ready annotation when this accepted Job reused cached text.';
COMMENT ON COLUMN item_summaries.created_at IS 'Time the annotation artifact was committed.';
COMMENT ON TABLE digest_item_summaries IS 'Separate late annotations for immutable Digest Items with ready bindings never replaced.';
COMMENT ON COLUMN digest_item_summaries.workspace_id IS 'Workspace owning the saved Issue and annotation.';
COMMENT ON COLUMN digest_item_summaries.issue_id IS 'Saved Issue whose immutable Item version is annotated.';
COMMENT ON COLUMN digest_item_summaries.item_id IS 'Captured Item identity within the saved Issue.';
COMMENT ON COLUMN digest_item_summaries.source_version IS 'Captured model-input version preventing newer canonical text from attaching.';
COMMENT ON COLUMN digest_item_summaries.job_id IS 'Shared accepted work with null meaning no request yet.';
COMMENT ON COLUMN digest_item_summaries.summary_id IS 'First ready annotation bound to the saved version.';
COMMENT ON COLUMN digest_item_summaries.requested_at IS 'Time the context marker or explicit binding was recorded.';
COMMENT ON TABLE item_summary_retries IS 'Permanent source Summary Job to successor bindings.';
COMMENT ON COLUMN item_summary_retries.workspace_id IS 'Workspace owning both Jobs.';
COMMENT ON COLUMN item_summary_retries.source_job_id IS 'Failed or cancelled Summary Job explicitly retried.';
COMMENT ON COLUMN item_summary_retries.successor_job_id IS 'Original admitted successor retained after terminal completion.';
COMMENT ON COLUMN item_summary_retries.created_at IS 'Time the Retry identity was accepted.';
