# Vector Triage Bot

Zero-config duplicate and similar detection for GitHub Issues and Pull Requests.

Vector Triage Bot runs as a GitHub Action, embeds new issue/PR content with GitHub Models, searches a SQLite index (vector + FTS), and posts a triage comment when matches are found.

## Quick Start

Create `.github/workflows/triage.yml` in your target repository:

```yaml
name: Triage

on:
  issues:
    types: [opened, edited]
  pull_request_target:
    types: [opened, synchronize]

permissions:
  contents: write
  issues: write
  pull-requests: write
  models: read

jobs:
  triage:
    runs-on: ubuntu-latest
    concurrency:
      group: triage-index
      cancel-in-progress: false
    steps:
      - uses: rizwankce/vector-triage@v1.0.2
```

That is the only file needed in the consumer repo.

## Why `pull_request_target`

Fork PRs need base-repo token permissions to:
- post triage comments
- update the index branch

`pull_request_target` runs in the base repository context. The action does not checkout or execute fork code; it only reads PR metadata (title/body/files/diff) as untrusted text.

## Inputs

| Input | Env Var | Default | Range | Description |
|---|---|---|---|---|
| `similarity-threshold` | `INPUT_SIMILARITY_THRESHOLD` | `0.75` | `0.0-1.0` | Minimum similarity shown in related table |
| `duplicate-threshold` | `INPUT_DUPLICATE_THRESHOLD` | `0.92` | `0.0-1.0` | Minimum similarity flagged as duplicate |
| `max-results` | `INPUT_MAX_RESULTS` | `5` | `1-20` | Max similar items to show |
| `index-branch` | `INPUT_INDEX_BRANCH` | `triage-index` | string | Branch used to persist `index.db` |

Example override:

```yaml
steps:
  - uses: rizwankce/vector-triage@v1.0.2
    with:
      similarity-threshold: "0.8"
      duplicate-threshold: "0.94"
      max-results: "8"
      index-branch: "triage-index"
```

## Behavior

- First run:
  - no index exists yet
  - item is indexed
  - no triage comment is posted
- No matches:
  - no new comment is posted
  - existing triage comment is removed if it became stale
- Matches found:
  - bot posts/updates one comment identified by `<!-- triage-bot:v1 -->`
  - duplicate warning appears when score >= duplicate threshold
  - similar table includes open/closed/merged states
- Errors:
  - action logs `::warning::...` and exits successfully for recoverable failures

## Security Model

- PR content is treated as untrusted text.
- No PR branch checkout in action runtime path.
- No `eval`/dynamic shell execution of PR content.
- Release binary is checksum-verified before execution.

Security checklist:
- `action.yml` has no `actions/checkout`
- `action.yml` verifies `sha256` before running binary
- event parser only reads JSON payload fields

## Release Model

- `action.yml` downloads prebuilt `triage-bot-linux-amd64` from GitHub Releases.
- `.github/workflows/release.yml` builds and uploads:
  - `triage-bot-linux-amd64`
  - `triage-bot-linux-amd64.sha256`

Cut a release by pushing a version tag, for example:

```bash
git tag v1.0.2
git push origin v1.0.2
```

## Testing

Local test commands:

```bash
go test ./...
go test ./internal/store/... -run Integration -v
```

CI workflows:
- `.github/workflows/test.yml` for unit + store integration tests
- `.github/workflows/e2e.yml` for scheduled/manual end-to-end validation

## E2E Validation

- Runbook: `docs/e2e-runbook.txt`
- Workflow: `.github/workflows/e2e.yml`

The E2E workflow validates duplicate/similar/no-match issue scenarios and uploads artifacts for audit.

## Troubleshooting and Performance

- Troubleshooting: `docs/troubleshooting.txt`
- Performance notes: `docs/perf-notes.txt`
