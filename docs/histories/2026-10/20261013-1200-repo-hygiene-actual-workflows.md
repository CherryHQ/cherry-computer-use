# Align repo hygiene checks with actual workflows

Request: `scripts/ci.sh` failed immediately because `check-repo-hygiene.sh` required workflow and config files that do not exist. Find out whether they were removed on purpose or never added, then fix whichever side is wrong.

- The missing files (`ci.yml`, `docs-check.yml`, `repo-hygiene.yml`, `supply-chain-security.yml`, `.markdownlint.json`, `.editorconfig`, PR/issue templates, `dependency-review-config.yml`) are absent from every ref. The required list was copied from the agent-first template in the bootstrap commit and never matched this repo, so the check had failed since then.
- [CI/CD](../../CICD.md) names `sdk-check.yml`, `sdk-distribution.yml` and `release.yml` as the real pipelines. The hygiene list now requires those workflows and only the template files that actually exist, instead of adding empty placeholder workflows.
- `check-action-pinning.sh` now skips local `uses: ./...` references. The reusable `sdk-distribution.yml` call in `release.yml` ships with the same commit and cannot be pinned to a SHA. Remote actions must still be pinned to a SHA.
- `scripts/ci.sh` now runs through docs, hygiene, pinning, script syntax, Linux Python tests, and Windows/Linux Go tests.

Files: `scripts/check-repo-hygiene.sh`, `scripts/check-action-pinning.sh`.
