# Use contract-first REST with advisory SSE

The backend exposes an OpenAPI 3.1 REST API as the source of truth and SSE for advisory invalidation only. Replayable event delivery and a generic realtime protocol were rejected because reconnecting clients can safely refresh authoritative resources.

Advisories include Feed invalidation, Digest readiness, and significant Job changes. Clients refetch
REST state on mount, reconnect, relevant events, or explicit Refresh. They do not rely on polling,
and a dropped advisory may leave loaded state stale until the next refresh trigger.

ADR 0008 extends delivery across replicas through PostgreSQL `LISTEN/NOTIFY`; the no-replay REST
authority described here is unchanged.

The generated Echo v5 registration wrapper owns every HTTP route. One handwritten `Server` embeds its validated `Dependencies` and implements `StrictServerInterface` in functional controller files, consuming generated request objects and returning typed response objects. `NewStrictHandler` adapts that implementation to generated route registration, keeping request binding and response encoding at the generated boundary. Construction fails if the Workspace ID or any service dependency is missing.

Controllers map generated DTOs to application capabilities and return generated success and problem responses, including version ETags and projection-pending results. A handwritten SSE response visitor preserves immediate flushing, heartbeats, and cancellation without buffering the stream; it cancels its subscription when writing ends. Image downloads use the generated binary response with the validated media type and `nosniff` header.

Transport handlers always delegate operation availability to a service. Operations present in the contract but not implemented yet return the shared `domain.ErrNotImplemented` from their service method; HTTP maps that error to Problem Details with `501 Not Implemented`. A missing service is a composition error, never a runtime substitute for an unimplemented operation.

Every request passes through `oapivalidator` using the OpenAPI document embedded in the generated package before it reaches a handler. Its normalized failures and generated binder errors are mapped to the public `Problem` schema. Contract-invalid input remains a `400 Bad Request`, while a missing required `If-Match` precondition remains `428 Precondition Required`, preserving the API's established behavior.

The strict adapter uses an Echo JSON serializer that preserves the former controllers' unknown-field and single-value checks on the body supplied by validation. Decode failures retain operation-specific problem types, and conditional updates retain precondition-error precedence. OpenAPI validation may apply schema defaults and re-encode a body before binding; this migration preserves that existing behavior.
