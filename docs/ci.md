# Continuous integration

The `CI` workflow is the single entry point for pull requests, pushes to `main`, manual runs
and release workflows. It uses `dorny/paths-filter` for routing and an always-run YAML gate for
one required status. There is no schedule and no performance or live-provider workload in CI.

## Checks

| Change or event | Checks |
| --- | --- |
| Backend paths | Backend lint, PostgreSQL/Qdrant integration suite and build |
| Plugin paths | Plugin typecheck, unit tests and production build |
| API/contract paths | Contract lint and generated-output reproducibility |
| Shared or unknown product paths | Backend, plugin and contract checks |
| Documentation-only paths | Tooling validation and the final gate only |
| Push to `main` | Selected backend suite uses the race detector |
| Manual/release invocation | `component` and `race` inputs force the requested checks |

Backend integration uses one PostgreSQL 17 and one Qdrant 1.20 service. The primary command
already includes ordinary Go tests, so CI does not run a duplicate full unit suite. Failed service
logs and JSON test output are retained for seven days.

The reusable interface is:

- `source_sha`: required full immutable checkout SHA;
- `component`: `none`, `backend`, `plugin` or `all`;
- `race`: whether the backend suite uses `-race`.

Release workflows call the same `ci.yml` workflow and force their component. Plugin preparation
dispatches the manual entry point with its exact prepared branch SHA.

## Required statuses

Require `CI / CI gate` in branch protection. The gate always runs after routing and component jobs:
selected jobs must be successful; unselected jobs may be skipped; failed, cancelled or missing
selected jobs fail the gate.

Commit policy is checked separately by `Commit checks`. Human PRs must contain one commit, a
conventional PR title and a conventional latest commit. Dependabot is excluded, matching the
reference `sx` workflow.

## Local checks

```bash
mise run test-ci
mise run test-tools
mise run lint-ci
mise run test-integration
mise run test-integration-race
pnpm install --frozen-lockfile
pnpm --dir plugin check
pnpm --dir plugin build
pnpm contract:lint
```

The repository does not claim that local checks establish GitHub runner, branch-protection or
repository-permission behavior. Verify the displayed required status after the first connected
run and remove the obsolete `Checks / CI gate` requirement.
