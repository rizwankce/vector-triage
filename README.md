# Vector Triage Bot

Zero-config duplicate and similar detection for GitHub Issues and Pull Requests.

Vector Triage Bot runs as a GitHub Action, embeds issue/PR content with GitHub Models, searches a local SQLite index (vector + FTS), and posts a triage comment when related items are found.

## Why This Project

- No external vector DB or hosted service
- No extra API key configuration
- Single workflow file to install
- Works for both issues and PRs
- Built for open source and private repos

## Quick Start

Create `.github/workflows/triage.yml` in the repository where you want triage:

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

That is the full setup.

## Configuration

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
      similarity-threshold: "0.80"
      duplicate-threshold: "0.94"
      max-results: "8"
      index-branch: "triage-index"
```

## How It Behaves

- First run:
  - no index exists yet
  - item is indexed
  - no triage comment is posted
- On later runs:
  - similar/duplicate matches produce one managed triage comment (`<!-- triage-bot:v1 -->`)
  - stale triage comments are updated or removed
- No matches:
  - no comment noise is added
- Recoverable failures:
  - logs `::warning::...`
  - exits non-fatally

## Security Notes

`pull_request_target` is required so fork PR events can comment and persist state with base-repo token permissions.

Safety model:
- PR content is treated as untrusted text only
- no PR branch checkout in action runtime path
- no dynamic `eval`/template execution from PR content
- downloaded release binary is SHA256-verified before execution

## Release Model

- `action.yml` downloads prebuilt `triage-bot-linux-amd64` from GitHub Releases.
- `.github/workflows/release.yml` publishes:
  - `triage-bot-linux-amd64`
  - `triage-bot-linux-amd64.sha256`

Cut a release:

```bash
git tag v1.0.2
git push origin v1.0.2
```

## Development and Validation

Local tests:

```bash
go test ./...
go test ./internal/store/... -run Integration -v
```

Key workflows:
- `.github/workflows/test.yml` for unit + store integration tests
- `.github/workflows/e2e.yml` for scheduled/manual E2E validation

Useful docs:
- `docs/e2e-runbook.txt`
- `docs/troubleshooting.txt`
- `docs/perf-notes.txt`

## Open Source

This is an open source project. Issues and pull requests are welcome.

## Acknowledgements

- Inspired by [`@tobi/qmd`](https://github.com/tobi/qmd) for SQLite + vector/hybrid retrieval patterns.
- Inspired by [`@similigh/simili-bot`](https://github.com/similigh/simili-bot) for GitHub bot workflow/state ideas.
- Special thanks to [steipete.me](https://steipete.me/) for the initial idea spark.

## License

MIT. See `LICENSE`.
