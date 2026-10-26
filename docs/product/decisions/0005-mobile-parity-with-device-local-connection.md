# Support mobile parity with a device-local backend connection

Status: Accepted
Date: 2026-09-27

Topic Stream v0.1 provides the same capabilities in Obsidian desktop and mobile through responsive layouts. The backend URL is stored per device rather than with synchronized vault settings because desktop and mobile commonly reach the same self-hosted daemon through different addresses.

## Consequences

Plugin code cannot depend on desktop-only Obsidian, Electron, Node.js, or filesystem APIs. Mobile compatibility is verified through responsive host frames and platform-safe boundaries in v0.1; physical-device smoke testing is deferred.
