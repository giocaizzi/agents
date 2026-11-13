# Release runbook

Releases are cut by hand with an annotated tag. No workflow opens release PRs: `release-please-config.json` and `.release-please-manifest.json` only record the versioning rules (`bump-minor-pre-major`, the last released version) for whoever picks the next version.

## Choose the version

- Before 1.0, a breaking change bumps the minor version (`0.2.1` → `0.3.0`); features and fixes bump the patch version.
- Breaking means a config key, file location, command, flag or exit status that users rely on changes or goes away. Commits marked `!` or with a `BREAKING CHANGE:` footer since the last tag are the list to check: `git log --oneline v<last>..main`.

## Cut it

1. On an up-to-date `main` with CI green: `git switch main && git pull --ff-only`.
2. Tag it, with release notes in the message (the highlights and the breaking changes, with what users must do):
   `git tag -a v0.3.0 -m "v0.3.0" -m "<notes>"`.
3. Push only the tag: `git push origin v0.3.0`.
4. The `release` workflow (`.github/workflows/release.yml`) runs on the tag. It builds `agents_<version>_<os>_<arch>` for darwin/amd64, darwin/arm64, linux/amd64, linux/arm64 and windows/amd64, writes `checksums.txt`, attests it, and publishes the GitHub release.
5. Check the release: all five binaries and `checksums.txt` are attached, and `agents version` on a downloaded binary prints the version.
6. Update `.release-please-manifest.json` to the new version in the next PR.

## Undo

A bad release cannot be republished with the same version. Delete the GitHub release and tag only if nobody has downloaded it yet; otherwise fix forward with the next patch version.
