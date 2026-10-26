# Documentation

Start with the guide that matches what you want to do.

| Goal | Start here |
| --- | --- |
| Install Topic Stream and get a first Feed | [Getting started](guides/getting-started.md) |
| Use the current plugin and find backend API capabilities | [Using Topic Stream](guides/using-topic-stream.md) |
| Configure the server or connect an AI provider | [Server configuration](operations/configuration.md) |
| Apply or inspect database migrations | [Database migrations](operations/database.md) |
| Diagnose a failed startup or empty Feed | [Troubleshooting](operations/troubleshooting.md) |
| Preserve or recover your data | [Backup and restore](operations/backup-and-restore.md) |
| Build, test, or contribute | [Development](development.md) |
| Run optional capacity or real-provider checks | [Local performance checks](performance.md) |
| Understand CI selection and required checks | [Continuous integration](ci.md) |
| Understand versions, preparation, archives and backend images | [Component releases](releases.md) |

## Product and engineering references

- [Product capabilities and decisions](product/README.md) describe behavior and the reasons behind it.
- [Product purpose and principles](../PRODUCT.md) explain the intended audience and positioning.
- [Domain vocabulary](../CONTEXT.md) defines shared terms.
- [Ranking pipeline](architecture/ranking-pipeline.md) explains retrieval, membership, scoring, and publication.
- [Jobs and runtime roles](architecture/jobs-and-runtime-roles.md) explains queues and process responsibilities.
- [Item projection](architecture/item-projection.md) explains derived-index delivery and recovery.
- [Item AI summaries](architecture/item-summaries.md) explains explicit generation, reuse and saved-version annotations.
- [Content ingestion and Reader delivery](architecture/content-ingestion-and-reader.md) explains extraction and image handling.
- [Architectural decisions](adr/README.md) preserve technical trade-offs and their replacement history.
- [OpenAPI contract](../api/openapi/openapi.yaml) is authoritative for HTTP paths, schemas, and errors.

## Where information belongs

Guides explain how to complete a task. Operations documents explain how to run and recover the server.
Capability references define product behavior; architecture documents explain its implementation.
ADR and product decision records preserve rationale, including superseded decisions.

Keep each detailed explanation in its owning document and link to it elsewhere. Public documents
must work in a fresh checkout without local specifications or issue files. Agent-specific conventions
remain in [domain documentation guidance](agents/domain.md) and [issue tracking guidance](agents/issue-tracker.md).
