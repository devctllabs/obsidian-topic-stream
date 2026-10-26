# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

The primary user is a technically proficient self-hoster: an engineer or researcher who uses Obsidian on desktop and mobile as a personal knowledge environment and is willing to run a reachable backend to control their information diet.

## Product Purpose

Topic Stream reduces information overload by turning user-selected sources into a personal, Topic-organized Feed and periodic Digest Issues. Success means the user consistently receives a small set of worthwhile Items, can understand why they appeared, and can improve future recommendations through explicit feedback without surrendering control of sources or data.

## Positioning

Topic Stream is an experimentation-friendly personal information retrieval engine, not merely an RSS reader and not an opaque recommendation service. Its differentiator is a configurable and explainable pipeline combining explicit Sources, natural-language Topics, replaceable retrieval and ranking stages, and current Like/Dislike feedback.

## Operating Context

The user runs the Topic Stream server as one all-in-one process or independently scaled HTTP,
scheduler, and worker replicas backed by PostgreSQL and a rebuildable Qdrant index. The responsive
Obsidian plugin is the primary interface on desktop and mobile. Each device keeps its own backend
connection URL while the user configures Sources and Topics, explicitly refreshes and reads a stable
Feed, provides Personal Feedback, receives scheduled or on-demand Digest Issues, and may create
independent Vault Snapshots.

`CONTEXT.md` is the authority for domain terminology. [Product capabilities](docs/product/README.md)
describe behavior and constraints; the [documentation index](docs/README.md) connects user guides,
operations, and engineering references.

## Capabilities and Constraints

- One user-owned Workspace scopes Sources, Topics, Items, preferences, and Digests; v0.1 has no authentication or multi-user collaboration.
- Sources normalize upstream entries into canonical Items. Topics add Source and Item-kind scope plus optional natural-language Include and Exclude criteria.
- Explicit current Like/Dislike feedback, optional comments, reading state, visibility, and Topic corrections remain separate user signals.
- Ranking is explainable and built from fixed, replaceable retrieval, scoring, reranking, and diversification stages rather than a generic workflow engine.
- PostgreSQL is authoritative; Qdrant and other retrieval representations are derived and rebuildable.
- AI text and preference data remain local. Embeddings and optional generation use operator-configured OpenAI-compatible local endpoints; cloud AI is outside v0.1.
- The v0.1 client is an English-only responsive Obsidian plugin with desktop and mobile feature parity. Content, Topic criteria, feedback comments, and summary language may be multilingual.
- Workspace Settings contain non-secret backend behavior. API keys and other credentials remain operator-managed secrets and never enter product APIs or portable archives.
- Reader content stays in the backend until the user explicitly creates an independent Vault Snapshot.

## Brand Commitments

The product name is Topic Stream. Product language is calm, direct, technically credible, and free of hype. The interface should help the user operate and understand the retrieval system without presenting unsupported intelligence claims.

The accepted visual direction is Signal Desk: a lightweight, precise reading workspace with
Readwise-inspired tonal restraint, a strong typographic hierarchy, cool neutrals, and a restrained
periwinkle-blue accent. Visual aesthetics are a core quality requirement, with equal care for
Obsidian light and dark modes. Yellow and ochre are not brand accents. The target direction is
recorded in [DESIGN.md](DESIGN.md); its pre-implementation seed does not claim the redesign is shipped.

## Evidence on Hand

The repository contains an accepted product specification, canonical domain vocabulary, architectural and product decisions, a working Go/HTTP/PostgreSQL foundation, and an early Obsidian plugin implementation. It has no customer testimonials, public usage claims, independent benchmarks, logo system, or marketing proof; future work must not fabricate them.

## Product Principles

1. The user controls the sources, criteria, schedules, and data boundary.
2. Recommendations remain inspectable and correctable instead of relying on opaque personalization.
3. Explicit feedback should make the product more useful over time without conflating preference, reading state, and visibility.
4. Reading is spatially stable and deliberate; background updates do not disrupt an active Feed session.
5. Canonical backend content and user-owned Vault Snapshots remain separate so the vault never changes implicitly.

## Accessibility & Inclusion

Plugin surfaces follow Obsidian's keyboard, semantic, focus, and theme conventions so they remain consistent with the host application. Topic Stream does not claim a separate formal WCAG conformance target for v0.1.
