# Component releases

Backend and plugin releases have independent stable version histories:

- backend tags are `backend/vX.Y.Z`;
- plugin tags are exactly `X.Y.Z`;
- the first published candidate for either component is `0.1.0`;
- `major`, `minor` and `patch` are explicit workflow inputs, not inferred by git-cliff.

Version calculation still reads published release history once through `release-version.yml`.
Drafts, prereleases and the other component's namespace do not become the baseline. A release
requires product changes belonging to that component or shared API/contract inputs. Documentation,
workflow-only and unclassified paths do not silently create a release: the path policy must be
updated before an unclassified product path can be released.

## Release notes

`cliff.toml` follows the compact conventional-commit grouping used by `sx`. `git-cliff` only
formats notes; it does not choose a version. Each release workflow supplies its explicit tag,
tag namespace and component paths:

- backend notes include backend runtime/build paths and shared contract paths;
- plugin notes include plugin/pnpm paths and shared contract paths;
- pnpm inputs are not backend release paths;
- docs and CI workflow changes are excluded from product notes.

The generated notes are written into the draft GitHub Release. Conventional commit quality is
enforced by `Commit checks`, while non-conventional history is still placed in git-cliff's `Other`
group rather than being dropped.

## Backend release

Run **Backend release** on `main` and select the explicit bump. The workflow freezes the dispatch
SHA, calculates the candidate, forces race-enabled CI and builds:

| Target | Archive |
| --- | --- |
| Linux amd64 | `topic-stream_X.Y.Z_linux_amd64.tar.gz` |
| macOS arm64 | `topic-stream_X.Y.Z_darwin_arm64.tar.gz` |

After checks and builds succeed, the workflow verifies the exact source/tag relationship, refuses
to overwrite an existing GitHub Release, creates `backend/vX.Y.Z` when needed and creates a draft
Release with `latest=false`. Publishing remains manual. The workflow does not resume an existing
draft or compare existing asset contents.

## Plugin release

Run **Plugin release** after plugin metadata has been prepared and reviewed. The workflow freezes
the dispatch SHA, forces plugin and contract checks, verifies that `package.json`, `manifest.json`
and `versions.json` agree, builds the plugin and uploads `main.js`, `manifest.json` and
`styles.css` to a draft `X.Y.Z` Release. It uses `latest=true` for the plugin release.
The downloaded asset manifest is compared byte-for-byte with the validated source manifest.
Its reused CI gate includes generated types, mobile dependency boundaries, Storybook and the
production-bundle headless Obsidian flows; see [plugin quality](plugin-quality.md).
An existing tag or draft/Release is rejected; the workflow does not resume an incomplete plugin
Release.

Plugin preparation remains separate: it creates one reviewable metadata PR, but it never tags,
publishes or auto-merges. An existing preparation branch or open PR is rejected without reuse or
overwrite; an already synchronized source is a no-op and proceeds directly to CI validation.

## Backend image

Run `publish-image` manually from `main` with a published backend tag such as
`backend/v0.1.0`. This workflow remains independent of release creation. It validates the
published backend Release, checks out the exact tag and publishes the versioned GHCR image for
`linux/amd64` and `linux/arm64`, including provenance and GitHub attestation verification.

## Permissions and local review

Ordinary CI is read-only. Only the final release jobs receive contents write permission; image
publication receives its scoped package, attestation and OIDC permissions. No actual tag,
Release, package or image is created by local verification.

```bash
python3 .github/scripts/compute_release_version.py --help
python3 .github/scripts/prepare_plugin_release.py --help
mise run test-ci
mise run lint-ci
```
