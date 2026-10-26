# Database migrations

Compose runs migrations in its one-shot `init` service before starting the server. For manual
inspection and controlled maintenance, override its shell entrypoint to run CLI commands:

```bash
docker compose run --rm --no-deps --entrypoint topic-stream init db status
docker compose run --rm --no-deps --entrypoint topic-stream init db up
```

Stop the server before a deliberate rollback, back up PostgreSQL first, and use `db down` only
when removing the corresponding schema and data is intended. See [backup and restore](backup-and-restore.md).

## Command behavior

The binary embeds the SQL in `migrations/primary/postgres`; it needs neither the checkout nor
`devctl.yaml` to migrate a database. The `db` commands only manage the schema and do not require or
create a Workspace. `serve` creates its selected Workspace and never applies migrations itself.

```bash
topic-stream db status
topic-stream db up
topic-stream db up --steps 2
topic-stream db down                 # rolls back one migration, removing its schema and data
topic-stream db down --steps 2
topic-stream db status --json        # version is the latest applied migration timestamp
topic-stream db up --url "$TOPIC_STREAM_DB_PRIMARY_POSTGRES_DSN" --json
```

The clean-install baseline contains eight timestamped up/down pairs in dependency order:

| Area | Owns |
| --- | --- |
| Workspace | Workspaces, model registry, settings, preferences and import markers |
| Content | Sources, Items, provenance, feedback and state |
| Topics | Scope, semantic revisions, membership corrections and gate calibration |
| Jobs | Durable execution, queue and active-request uniqueness |
| Retrieval | Collections, model progress, projections, rebuild cohorts and recovery linkage |
| Digests | Definitions, immutable Issues, consumption and Build identities |
| Feed | Shared publication history, display entries and prepared candidates |
| Item AI summaries | Exact inputs, annotations, cache and Retry bindings |

Rollback removes these areas in reverse order. Scoped uniqueness belongs to its owning table,
and the Summary input/result reference cycle is removed explicitly before its tables are dropped.
The baseline includes scoped projection foreign keys, `fillfactor=90` on projection state/pending
work, and Feed topic-array statistics of 1000. Registry/default model data is created by the
application rather than seeded by migrations.

The unpublished pre-release history was consolidated for **clean installation only**. A development
database initialized with an earlier migration set must be deliberately recreated before applying
this baseline. There is no legacy-history conversion or data-preserving upgrade for that rewrite.
Existing databases, volumes and migration metadata are never reset or forced automatically.
Keep old data with its matching application revision if it must be retained; do not point this
baseline at an old-history database or restored backup. See [backup and restore](backup-and-restore.md).

`--url` overrides `TOPIC_STREAM_DB_PRIMARY_POSTGRES_DSN` from the process environment, which overrides
the optional `.env` in the current directory. An explicit empty value fails. These commands load
only the primary PostgreSQL settings; unrelated API, worker, Workspace, and Qdrant settings are
not required. Devctl's `primary:postgres` migration target uses the same DSN variable.

`up` without `--steps` applies all pending migrations; repeating it succeeds with `noChange=true`.
Explicit step counts must be positive. If fewer steps are available than requested, the engine can
execute those available and then return an error. `down` on an unapplied database fails.

JSON output is a successful result only: `version` is `null` before the first migration, `dirty`
is a boolean, and `up/down` also return `noChange`. Errors go to stderr with a nonzero exit code;
migration errors include the observed version and dirty state, or `unknown` if unavailable.
`status` succeeds for a dirty database, while `up/down` refuse to proceed. Recovery requires an
explicit operator decision; there is no automatic rollback or force command. Status construction
may create the migration history table. Started SQL follows the driver's cancellation semantics;
configure connection and statement timeouts in the DSN as needed.


See [configuration](configuration.md) for host and container connection addresses.
