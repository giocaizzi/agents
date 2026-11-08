# <repo name>

<One line: what this repo is for.>

- Commands: `make help`, `make lint`, `make test`, `make check` (= lint + test; CI job `check` runs it), `make prepush` (the local gate before marking a PR ready).
- Docs live in [docs/](docs/); this file links to them and never duplicates them.
- GitHub setup follows the [GitHub standard](https://github.com/giocaizzi/.github/blob/main/docs/standard.md).
- `SECURITY.md` and issue templates are inherited from `giocaizzi/.github`; do not add copies.

## Onboarding
Checklist for a new repo; rules and tiers are in the [standard](https://github.com/giocaizzi/.github/blob/main/docs/standard.md), procedure in its [rollout runbook](https://github.com/giocaizzi/.github/blob/main/docs/runbook-rollout.md).
- Add the repo to `standard/repos.yaml` in `giocaizzi/.github` (`type`, `released`, `pypi`, `deploys`), then `make onboard REPO=<name>` there.
- Python: ruff `select = ["ALL"]` with Google docstrings from the start (snippet in the harness `python-development` skill and the standard).
- Released: release-please. PyPI: trusted publishing from environment `pypi`, and register the trusted publisher on pypi.org.
- Publish or deploy jobs: run in a GitHub environment, secrets as environment secrets.
- README badges grow by tier (standard, README).

## Gotchas
- <Add repo-specific gotchas here.>
