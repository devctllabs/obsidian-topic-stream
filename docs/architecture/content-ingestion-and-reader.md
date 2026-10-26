# Content ingestion and Reader delivery

PostgreSQL stores canonical Items and Source Entry provenance. Source adapters fetch and normalize
upstream data; derived search projection is a separate responsibility described in
[Item projection](item-projection.md).

## Source extraction

RSS and Atom entries are normalized into Items. Tracking parameters (`utm_*`, `fbclid`, and `gclid`) and URL fragments are removed from the canonical URL. Each linked article is fetched through the controlled SSRF-aware HTTP client with a bounded response size, decoded according to its declared or detected charset, cleaned with Readability, and deterministically converted to canonical Markdown. Readability normalizes relative content links against the article URL. Every Source Entry retains compact typed source-specific details and its extraction version. Full article content and repository README remain on the canonical Item instead of being duplicated for every Source Entry. Repeated synchronization is idempotent by Workspace canonical URL.

The extraction pipeline is versioned as `readability-v1`; older rows are marked `legacy-v1` by migration. Extraction and fetch failures fail the owning durable synchronization Job, follow the Fetch Queue retry policy, and remain visible through the Job diagnostic reason. PostgreSQL leases, heartbeat, and lease-expiry recovery make interrupted synchronization work reclaimable.

arXiv Atom entries become typed Paper Items using their abstract, authors, categories, DOI, and publication time. YouTube Atom entries become typed Video Items using the channel and video identifiers plus `media:description`; v0.1 does not acquire transcripts. The strict version-1 GitHub Trending snapshot becomes typed Repository Items. On first discovery, the adapter fetches repository metadata and README from the GitHub API and records extraction version `github-trending-v2`. Later Trending polls reuse that stored enrichment without another GitHub API call while refreshing snapshot-specific rank, period, observation, and stars-in-period values. Repository cards receive typed homepage, topic, open-issue, and default-branch fields; README content is exposed once through the Item's canonical `contentMarkdown`.

## Reader images

The Item API returns cleaned canonical Markdown and deterministic descriptors derived only from
safe absolute HTTP(S) image references in that content. Retrieving an Item does not download images.
The image endpoint accepts only a descriptor ID belonging to that Item and fetches through the
SSRF-aware controlled HTTP client. It requires a successful response with an image media type and
enforces a 10 MB limit. Responses use the validated media type and `X-Content-Type-Options: nosniff`.
SVG is eligible; the Obsidian save workflow applies its required strict SVG sanitizer before writing it.

HTTP paths and payloads are defined in the [OpenAPI contract](../../api/openapi/openapi.yaml).
See [Search and Reader behavior](../product/capabilities/search-reader-and-events.md) for the user-facing boundary.
