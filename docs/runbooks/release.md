# Release automation

**When:** Release a version through release-please after eligible Conventional
Commits land on `main`. Use the emergency recovery procedure only when
release-please is broken or an emergency release is genuinely required; it is
not for ordinary releases.

**Preconditions:** You have the required approval and repository access. The
change being released has passed the repository checks, including `make check`
where appropriate. You understand that the Conventional Commit type controls
the version increment and changelog section.

## Steps

### Ordinary release

1. Use the appropriate Conventional Commit type for each change. The type
   determines the release version increment and changelog section.
2. Land the eligible changes on `main` through the normal review process. Do
   not manually create or push a release tag for an ordinary release.
3. Wait for release-please to create or update its Release PR from the new
   `main` history.
4. Review and merge the Release PR. Merging it is the ordinary release: it
   creates the release tag and triggers the release build and publication
   workflow.
5. Do not hand-edit generated release state during ordinary release work.
   release-please owns `CHANGELOG.md` and `.release-please-manifest.json`.
   Correct the manifest manually only to reconcile historical version drift.

### Emergency recovery

1. Confirm that release-please is broken or that an emergency cut is genuinely
   needed, and obtain the required approval and repository access.
2. Reconcile version-bearing and generated release state before cutting the
   tag, including `CHANGELOG.md` and `.release-please-manifest.json`. Do not
   use this procedure as a shortcut for a normal release.
3. Create an annotated tag for the reconciled version and push it to invoke the
   emergency fallback workflow:

   ```sh
   git tag -a vX.Y.Z -m "vX.Y.Z"
   git push origin vX.Y.Z
   ```

4. After recovery, land a later eligible change on `main` and confirm that
   release-please again creates or updates its Release PR.

## Verify

### Ordinary release verification

1. Confirm that release-please has created or updated the Release PR after the
   `main` push.
2. Before merging the Release PR, confirm its generated version and changelog
   entries match the Conventional Commits being released.
3. Confirm that merging the Release PR creates the GitHub Release for
   `vX.Y.Z`, with the expected release assets and an artifact attestation.
4. Run the repository check when validating release changes locally:

   ```sh
   make check
   ```

### Emergency recovery verification

1. Confirm that the pushed `vX.Y.Z` tag starts the emergency manual-tag release
   fallback workflow.
2. Confirm that the workflow creates the GitHub Release for `vX.Y.Z`, attaches
   the cross-platform release assets and checksums, and publishes an artifact
   attestation.
3. Confirm that a subsequent eligible `main` push recreates or updates the
   release-please Release PR.

## Rollback

Before pushing an emergency tag, delete the local tag and correct the
reconciled release state:

```sh
git tag -d vX.Y.Z
```

After a tag has been pushed or a GitHub Release has been published, do not
delete or retag it as routine rollback. Reconcile the release state and issue a
corrected forward release through the approved recovery process.

**Last verified:** 2026-06-12 by reviewing `release-please-config.json` and the
emergency fallback workflow in `.github/workflows/release.yml`.
