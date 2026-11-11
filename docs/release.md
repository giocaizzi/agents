# Release runbook

**When:** releasing a version of `agents`. Releases are automated with release-please; the manual annotated tag is a fallback for when release-please is broken or an emergency release is needed.

**Preconditions:** repository access and approval to merge to `main`; CI green on `main`. The Conventional Commit type of each change drives the version bump and the changelog section.

## How it works

- `.github/workflows/release-please.yml` runs on every push to `main`. It opens or updates a Release PR from the Conventional Commits since the last tag, using `release-please-config.json` (`bump-minor-pre-major`) and `.release-please-manifest.json` (the last released version).
- Merging the Release PR tags `vX.Y.Z` and creates the GitHub release. The same workflow then builds `agents_<version>_<os>_<arch>` for darwin/amd64, darwin/arm64, linux/amd64, linux/arm64 and windows/amd64, writes `checksums.txt`, attests it and uploads the assets to the release. Uploads are retry-safe: re-running the job replaces assets already attached.
- A tag pushed by release-please with the workflow token does not start other workflows, which is why publishing lives in `release-please.yml` and `release.yml` only handles the fallback tag.
- Before 1.0, a breaking change (a config key, file location, command, flag or exit status users rely on) bumps the minor version (`0.3.0` to `0.4.0`); features and fixes bump the patch version.

## Steps

### Ordinary release

1. Land changes on `main` with the right Conventional Commit type (`!` or a `BREAKING CHANGE:` footer for breaking ones).
2. Wait for release-please to open or update the Release PR.
3. Check its version and changelog against `git log --oneline v<last>..main`, then merge it. Do not push a tag by hand and do not edit `CHANGELOG.md` or `.release-please-manifest.json`; release-please owns them.

### Fallback: manual tag

Only when release-please is broken or an emergency release is needed.

1. On an up-to-date `main` with CI green: `git switch main && git pull --ff-only`.
2. Set `.release-please-manifest.json` to the new version and merge that, so release-please continues from it.
3. Tag with release notes in the message (highlights and breaking changes, with what users must do): `git tag -a vX.Y.Z -m "vX.Y.Z" -m "<notes>"`.
4. Push only the tag: `git push origin vX.Y.Z`.
5. `.github/workflows/release.yml` runs on the tag, builds and attests the same assets, and creates (or, on a re-run, updates) the GitHub release.

## Verify

1. After a push to `main`, the Release PR exists or is updated.
2. After merging it (or pushing the fallback tag), the GitHub release `vX.Y.Z` has all five binaries and `checksums.txt`, and an artifact attestation.
3. `agents version` on a downloaded binary prints the version.
4. After a fallback release, a later push to `main` updates the Release PR from the new manifest version.

## Rollback

A bad release cannot be republished with the same version. Delete the GitHub release and tag only if nobody has downloaded it yet; otherwise fix forward with the next patch version. Before pushing a fallback tag, undo it with `git tag -d vX.Y.Z`.
