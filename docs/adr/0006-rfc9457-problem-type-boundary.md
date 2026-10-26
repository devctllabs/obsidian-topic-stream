# ADR 0006: RFC 9457 problem type boundary

## Status

Accepted.

## Context

The public HTTP API uses `application/problem+json`. A blanket `type: about:blank` paired with application-specific titles such as `Settings changed` is contradictory: RFC 9457 defines `about:blank` as having no semantics beyond the HTTP status and recommends the standard status phrase as its title. Raw service errors in a 5xx response can also disclose storage, network, or credential details.

## Decision

OpenAPI request-boundary failures keep the stable problem types supplied by `oapivalidator`.

Known Topic Stream failures use a stable `urn:topic-stream:problem:<slug>` type and matching snake_case `code`. The title is stable for that type; the detail describes the occurrence.

The HTTP transport owns an explicit catalog mapping semantic error categories to status, code, type, and safe English title/detail. Codes never depend on display strings. Domain validation errors carry field paths, rule identifiers, and approved constraint facts, without public messages or submitted values. Independent violations are collected at each reached validation stage before proceeding to writes or later operations. The transport renders them as `errors[{field,message}]`; individual violations do not introduce public machine codes.

OpenAPI violations retain validator code/type values and the default reporting limit of 20 violations. Transport maps structured schema constraints to safe messages for required values, types, ranges, lengths, choices, and formats; unsupported constraints have a generic fallback. Raw schema errors and decoder strings are not public messages.

The initial Topic Stream naming baseline deliberately replaces the previous product namespace without compatibility aliases. This is a public-contract change; the stability guarantee applies to the namespace above.

`about:blank` is reserved for generic HTTP failures whose status code is the complete machine-readable meaning. Its title is the standard HTTP status phrase.

Unknown application failures propagate as Go errors through the strict adapter to the shared HTTP error handler. It returns `500`, `code: internal_error`, `type: urn:topic-stream:problem:internal-error`, and the fixed detail `The server could not complete the request.` The underlying cause remains in the Go error chain. Request-level logging records bounded diagnostic facts once (error/cause types, method, matched route, and SQLSTATE when available), excluding arbitrary error text, credentials, and request bodies.

Expected failures return generated strict response objects. Known operational outcomes keep their established status, code, and approved messages, including invalid upstream images and `projection_pending`. The latter preserves `canonicalCommitted` and `projectionRevision`; it must not imply the write rolled back.

The Obsidian client parses Problem Details centrally. UI displays detail, falling back to title, and a list of field messages as plain text. A committed projection failure is presented as a saved change. Network failures and invalid error payloads receive safe client fallbacks; writes are not automatically retried. Existing machine codes and validator problem types remain compatible.

## Consequences

Clients may branch on `type` or `code`, never on human-readable `detail`. Adding or changing a Topic Stream problem type is a public-contract change. Unknown dependency failures must not be classified as user validation errors, and raw error strings must not be exposed in any HTTP response.
