# Release runbook

**When:** releasing a version of `agents`. Releases are manual (tag push) today. release-please is merged but dormant until it is activated (see "Activating release-please").

**Preconditions:** repository access; CI green on an up-to-date `main`. The Conventional Commit type of each change drives the version bump.

## How it works

- Exactly one workflow publishes, chosen by the repo variable `RELEASE_PLEASE`:
  - not `true` (current): `.github/workflows/release.yml` publishes on a pushed `v*` tag; `.github/workflows/release-please.yml` does nothing.
  - `true`: `release-please.yml` is the only publisher; `release.yml` does nothing.
- Either publisher builds `agents_<version>_<os>_<arch>` for darwin/amd64, darwin/arm64, linux/amd64, linux/arm64 and windows/amd64, writes `checksums.txt`, attests it, and creates the release as a draft, uploads the assets, then publishes it, so "latest" never points at a release without assets. Uploads are retry-safe: use "Re-run failed jobs" and assets already attached are replaced.
- With release-please active, it opens or updates a Release PR from the Conventional Commits since the last tag, using `release-please-config.json` (`bump-minor-pre-major`) and `.release-please-manifest.json` (the last released version). Merging the Release PR tags `vX.Y.Z` and creates the draft release. It runs with a GitHub App token, because a tag pushed with the workflow token would not start other workflows.
- Before 1.0, a breaking change (a config key, file location, command, flag or exit status users rely on) bumps the minor version (`0.3.0` to `0.4.0`); features and fixes bump the patch version.

## Steps

### Manual release (current)

1. On an up-to-date `main` with CI green: `git switch main && git pull --ff-only`.
2. Tag with release notes in the message (highlights and breaking changes, with what users must do): `git tag -a vX.Y.Z -m "vX.Y.Z" -m "<notes>"`.
3. Push only the tag: `git push origin vX.Y.Z`. `release.yml` builds, attests and publishes the release.
4. After it succeeds, set `.release-please-manifest.json` to `X.Y.Z` in a follow-up commit on `main`, so release-please continues from it once activated. No `CHANGELOG.md` entry is written for manual releases.

### Activating release-please

1. Create a GitHub App with repository permissions Contents: write and Pull requests: write, and install it on this repository.
2. Add the repository secrets `RP_APP_ID` (the App ID) and `RP_APP_KEY` (a generated private key).
3. Check `.release-please-manifest.json` matches the latest released version.
4. Set the repository variable `RELEASE_PLEASE` to `true`. From then on `release.yml` stops publishing; do not push tags by hand.
5. Release: land changes with the right Conventional Commit type, wait for the Release PR, check its version and changelog against `git log --oneline v<last>..main`, merge it. Do not edit `CHANGELOG.md` or `.release-please-manifest.json`; release-please owns them.

To return to manual releases, set `RELEASE_PLEASE` to anything other than `true`.

## Verify

1. The GitHub release `vX.Y.Z` is published with all five binaries and `checksums.txt`, and has an artifact attestation.
2. `agents version` on a downloaded binary prints the version.
3. With release-please active, a push to `main` creates or updates the Release PR; after a manual release the manifest names the new version.

## Rollback

A bad release cannot be republished with the same version. Delete the GitHub release and tag only if nobody has downloaded it yet; otherwise fix forward with the next patch version. Before pushing a tag, undo it with `git tag -d vX.Y.Z`.
