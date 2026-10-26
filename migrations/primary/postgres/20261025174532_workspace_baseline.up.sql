CREATE TABLE workspaces (
    id uuid PRIMARY KEY,
    created_at timestamptz NOT NULL DEFAULT now()
);

COMMENT ON TABLE workspaces IS 'User-owned scopes that contain all Topic Stream data.';
COMMENT ON COLUMN workspaces.id IS 'Stable identifier of the Workspace.';
COMMENT ON COLUMN workspaces.created_at IS 'Time when the Workspace was created.';

CREATE TABLE embedding_models (
    id uuid PRIMARY KEY,
    name text NOT NULL UNIQUE,
    endpoint text NOT NULL,
    provider_model text NOT NULL,
    dimensions integer NOT NULL CONSTRAINT embedding_models_dimensions_check CHECK (dimensions BETWEEN 1 AND 65536),
    enabled boolean NOT NULL DEFAULT false,
    initialization_status text NOT NULL CONSTRAINT embedding_models_status_check CHECK (
        initialization_status IN ('initializing', 'ready', 'failed')
    ),
    failure_reason text,
    version integer NOT NULL CONSTRAINT embedding_models_version_check CHECK (version >= 1),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

COMMENT ON TABLE embedding_models IS 'Server-managed non-secret descriptions of embedding providers and immutable vector shapes.';
COMMENT ON COLUMN embedding_models.id IS 'Stable identifier selected by Workspace Settings.';
COMMENT ON COLUMN embedding_models.name IS 'Unique operator-facing model name.';
COMMENT ON COLUMN embedding_models.endpoint IS 'OpenAI-compatible embeddings endpoint without credentials.';
COMMENT ON COLUMN embedding_models.provider_model IS 'Immutable model name sent to the embedding provider.';
COMMENT ON COLUMN embedding_models.dimensions IS 'Immutable number of values in every dense vector.';
COMMENT ON COLUMN embedding_models.enabled IS 'Whether a ready model may be selected for a new Workspace transition.';
COMMENT ON COLUMN embedding_models.initialization_status IS 'Latest synchronous provider and Qdrant initialization outcome.';
COMMENT ON COLUMN embedding_models.failure_reason IS 'Sanitized initialization failure safe to expose through the admin API.';
COMMENT ON COLUMN embedding_models.version IS 'Optimistic concurrency version.';
COMMENT ON COLUMN embedding_models.created_at IS 'Time when the model was registered.';
COMMENT ON COLUMN embedding_models.updated_at IS 'Time when mutable state last changed.';

CREATE TABLE workspace_settings (
    workspace_id uuid PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
    embedding_model_id uuid NOT NULL REFERENCES embedding_models(id),
    document jsonb NOT NULL,
    version integer NOT NULL CONSTRAINT workspace_settings_version_check CHECK (version >= 1),
    updated_at timestamptz NOT NULL
);

COMMENT ON TABLE workspace_settings IS 'Current non-secret settings document for each Workspace.';
COMMENT ON COLUMN workspace_settings.workspace_id IS 'Workspace that owns the settings document.';
COMMENT ON COLUMN workspace_settings.embedding_model_id IS 'Desired Embedding Model selected by the Workspace.';
COMMENT ON COLUMN workspace_settings.document IS 'Typed Workspace Settings encoded as JSON.';
COMMENT ON COLUMN workspace_settings.version IS 'Optimistic concurrency version of the settings document.';
COMMENT ON COLUMN workspace_settings.updated_at IS 'Time when the settings document was last changed.';

CREATE TABLE preference_profiles (
    workspace_id uuid PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
    prefer text NOT NULL DEFAULT '',
    avoid text NOT NULL DEFAULT '',
    revision integer NOT NULL CONSTRAINT preference_profiles_revision_check CHECK (revision >= 1),
    version integer NOT NULL CONSTRAINT preference_profiles_version_check CHECK (version >= 1),
    updated_at timestamptz NOT NULL
);

COMMENT ON TABLE preference_profiles IS 'Current user-written Prefer and Avoid guidance for each Workspace.';
COMMENT ON COLUMN preference_profiles.workspace_id IS 'Workspace that owns the current Preference Profile.';
COMMENT ON COLUMN preference_profiles.prefer IS 'Natural-language guidance describing preferred content.';
COMMENT ON COLUMN preference_profiles.avoid IS 'Natural-language guidance describing content to avoid.';
COMMENT ON COLUMN preference_profiles.revision IS 'Immutable semantic revision referenced by historical results.';
COMMENT ON COLUMN preference_profiles.version IS 'Optimistic concurrency version of the current profile.';
COMMENT ON COLUMN preference_profiles.updated_at IS 'Time when the current profile was last changed.';

CREATE TABLE preference_profile_revisions (
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    revision integer NOT NULL CONSTRAINT preference_profile_revisions_revision_check CHECK (revision >= 1),
    prefer text NOT NULL DEFAULT '',
    avoid text NOT NULL DEFAULT '',
    version integer NOT NULL CONSTRAINT preference_profile_revisions_version_check CHECK (version >= 1),
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (workspace_id, revision)
);

COMMENT ON TABLE preference_profile_revisions IS 'Immutable history of Preference Profile revisions.';
COMMENT ON COLUMN preference_profile_revisions.workspace_id IS 'Workspace that owns the historical profile revision.';
COMMENT ON COLUMN preference_profile_revisions.revision IS 'Semantic revision number within the Workspace.';
COMMENT ON COLUMN preference_profile_revisions.prefer IS 'Preferred-content guidance captured by this revision.';
COMMENT ON COLUMN preference_profile_revisions.avoid IS 'Avoided-content guidance captured by this revision.';
COMMENT ON COLUMN preference_profile_revisions.version IS 'Current-profile version that produced this revision.';
COMMENT ON COLUMN preference_profile_revisions.updated_at IS 'Time when this immutable revision was created.';

CREATE TABLE workspace_imports (
    workspace_id uuid PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
    format_version text NOT NULL,
    exported_at timestamptz NOT NULL,
    imported_at timestamptz NOT NULL DEFAULT now()
);

COMMENT ON TABLE workspace_imports IS 'Committed product archive import marker for each Workspace.';
COMMENT ON COLUMN workspace_imports.workspace_id IS 'Workspace populated by the committed import.';
COMMENT ON COLUMN workspace_imports.format_version IS 'Product archive format version that was imported.';
COMMENT ON COLUMN workspace_imports.exported_at IS 'Export time recorded by the imported archive.';
COMMENT ON COLUMN workspace_imports.imported_at IS 'Time when the archive import committed locally.';
