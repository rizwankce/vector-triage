# Vector Triage Bot — Technical Specification

> Zero-config duplicate detection for GitHub Issues and Pull Requests.
> Drop one workflow YAML file into any repo. No API keys, no external services, no configuration.

---

## 1. Vision & Constraints

### 1.1 What This Is

A GitHub Action that automatically detects duplicate and similar Issues/PRs when they are opened or updated. It posts a triage comment with:
- A **duplicate warning** if a near-identical item exists (≥92% display similarity)
- A **similar items table** showing related Issues/PRs (≥75% display similarity)

### 1.2 Hard Constraints

| Constraint | Implication |
|-----------|-------------|
| **Zero external services** | No Qdrant, Pinecone, Redis, or any hosted DB |
| **Zero API keys to configure** | Use only `GITHUB_TOKEN` (auto-provided by Actions) |
| **Zero config files** | Sensible defaults, no `.yml` config in target repo |
| **Single workflow file** | User drops one `.github/workflows/triage.yml` — done |
| **Works on public & private repos** | Must use GitHub Models API (free for both via `GITHUB_TOKEN`) |
| **Stateless runners** | Action runners are ephemeral — state must persist elsewhere |

### 1.3 User Experience (The Entire Setup)

```yaml
# .github/workflows/triage.yml — THE ONLY FILE THE USER CREATES
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
      - uses: your-org/triage-bot@v1
```

> **Why `pull_request_target` instead of `pull_request`**: The `pull_request` event runs
> in the context of the fork for fork PRs, which means `GITHUB_TOKEN` has read-only
> permissions — it cannot post comments or push to the orphan branch. `pull_request_target`
> runs in the context of the base repository, so the token has full write permissions
> even for fork PRs. See Section 8.6 for security implications.

---

## 2. Technology Choices

### 2.1 Language: Go

**Rationale:**

- **Single static binary** — no runtime to install (unlike Node/Python/Bun)
- **~5ms cold start** — critical for Action speed (vs 30-60s for Python dependency install)
- **`google/go-github`** — best-in-class GitHub API client
- **`mattn/go-sqlite3`** — mature SQLite driver with extension support
- **`asg017/sqlite-vec-go-bindings`** — Go bindings for sqlite-vec
- **Cross-compilation** — `GOOS=linux GOARCH=amd64` builds for every Actions runner
- **~15MB binary** — small Docker image, fast pull

### 2.2 Action Distribution: Pre-built Binary (not Docker build)

**Rationale:**

Docker-based actions that build from source (`runs.using: 'docker'` with a `Dockerfile`)
require building the image on every run (~60-90s for Go + CGo + sqlite). This breaks the
<30s runtime target.

Instead, use a **pre-built binary** distributed as a GitHub Release asset, downloaded at
action runtime by a thin composite action wrapper:

```yaml
# action.yml — composite action, NOT docker build
runs:
  using: 'composite'
  steps:
    - name: Download triage-bot binary
      shell: bash
      run: |
        RELEASE_URL="https://github.com/your-org/triage-bot/releases/download/v1/triage-bot-linux-amd64"
        curl -sL "$RELEASE_URL" -o /tmp/triage-bot
        chmod +x /tmp/triage-bot

    - name: Run triage
      shell: bash
      run: /tmp/triage-bot
      env:
        INPUT_SIMILARITY_THRESHOLD: ${{ inputs.similarity-threshold }}
        INPUT_DUPLICATE_THRESHOLD: ${{ inputs.duplicate-threshold }}
        INPUT_MAX_RESULTS: ${{ inputs.max-results }}
        INPUT_INDEX_BRANCH: ${{ inputs.index-branch }}
```

The binary is pre-compiled with CGo and sqlite-vec statically linked. Release builds are
automated via a separate CI workflow in the triage-bot repo.

**Alternative (v1.1):** Publish as a container action with a pre-built image on GHCR
(`image: 'docker://ghcr.io/your-org/triage-bot:v1'`). This avoids build time entirely
and pulls the cached image in ~5s. Deferred to v1.1 because it adds a container registry
dependency to the release process.

### 2.3 Vector Storage: SQLite + sqlite-vec

**Rationale:**

Based on the approach used by [`tobi/qmd`](https://github.com/tobi/qmd) (production-proven on-device search engine). Key learnings applied:

- Single `.sqlite` file contains everything (vectors, FTS index, metadata)
- `sqlite-vec` extension provides cosine similarity search via virtual tables
- **CRITICAL BUG**: sqlite-vec virtual tables hang indefinitely when combined with JOINs in the same query. Must use a **two-step query pattern** (vector search first, then separate metadata lookup). See: https://github.com/tobi/qmd/pull/23
- `FTS5` provides BM25 full-text search for hybrid retrieval
- Cosine distance metric: `distance_metric=cosine` in table definition

### 2.4 Embeddings: GitHub Models API

**Rationale:**

- Available for **free** in GitHub Actions via the `GITHUB_TOKEN`
- No additional API keys or secrets required
- Endpoint: `https://models.inference.ai.azure.com/embeddings`
- Model: `text-embedding-3-small` (1536 dimensions)
- Auth: `Authorization: Bearer $GITHUB_TOKEN`

### 2.5 State Persistence: Git Orphan Branch

**Rationale:**

Based on the pattern used by [`similigh/simili-bot`](https://github.com/similigh/simili-bot) for persisting state without external services. The bot stores its SQLite database on a dedicated orphan branch (`triage-index`) in the same repository.

- No external storage needed
- Survives across ephemeral Action runs
- A repo with ~5,000 issues + ~2,000 PRs produces an `index.db` of ~50MB — well within Git's limits
- Orphan branch has no common history with `main` — doesn't pollute the repo

---

## 3. Architecture

### 3.1 High-Level Flow

```
┌─────────────────────────────────────────────────────────────────────┐
│                         GitHub Event                                 │
│         (issues.opened / pull_request_target.opened)                 │
└──────────────────────────────┬──────────────────────────────────────┘
                               │
                               ▼
                    ┌─────────────────────┐
                    │   1. Pull State     │
                    │   (git fetch        │
                    │    orphan branch    │
                    │    → index.db)      │
                    └──────────┬──────────┘
                               │
                               ▼
                    ┌─────────────────────┐
                    │   2. Ingest Event   │
                    │   Parse issue/PR    │
                    │   into embeddable   │
                    │   text content      │
                    └──────────┬──────────┘
                               │
                               ▼
                    ┌─────────────────────┐
                    │   3. Embed          │
                    │   GitHub Models API │
                    │   → float[1536]     │
                    └──────────┬──────────┘
                               │
                    ┌──────────┴──────────┐
                    ▼                     ▼
          ┌──────────────────┐  ┌──────────────────┐
          │ 4a. Vector Search│  │ 4b. FTS Search   │
          │ (sqlite-vec)     │  │ (BM25 via FTS5)  │
          │ Cosine similarity│  │ Keyword match    │
          └────────┬─────────┘  └────────┬─────────┘
                   │                     │
                   └──────────┬──────────┘
                              ▼
                   ┌─────────────────────┐
                   │ 5. Rank Fusion      │
                   │ Compute display     │
                   │ similarity from     │
                   │ best source score   │
                   └──────────┬──────────┘
                              │
                              ▼
                   ┌─────────────────────┐
                   │ 6. Duplicate        │
                   │    Detection        │
                   │ (display sim ≥ 0.92 │
                   │  → flag duplicate)  │
                   └──────────┬──────────┘
                              │
                              ▼
                   ┌─────────────────────┐
                   │ 7. Index New Item   │
                   │ (upsert into        │
                   │  sqlite-vec + FTS)  │
                   └──────────┬──────────┘
                              │
                              ▼
                   ┌─────────────────────┐
                   │ 8. Post Comment     │
                   │ (GitHub API:        │
                   │  triage report)     │
                   │ Only if matches     │
                   │ found. Silent       │
                   │ no-op otherwise.    │
                   └──────────┬──────────┘
                              │
                              ▼
                   ┌─────────────────────┐
                   │ 9. Push State       │
                   │ (git push index.db  │
                   │  to orphan branch)  │
                   └─────────────────────┘
```

### 3.2 Embedding Content Strategy

Different content is embedded depending on the event type:

**Issues:**
```
Issue: {title}

{body}
```

**Pull Requests:**
```
PR: {title}

Description: {body}

Files changed: {comma-separated file paths}

Diff summary: {first 4000 chars of diff, or summarized}
```

**Diff truncation rule:** Always cap at **4000 characters**. If the raw diff exceeds this,
take the first 4000 characters. Always include the full file path list regardless of diff
size (file paths are small and high-signal for PR similarity).

For PRs, file paths are critical — two PRs touching the same files are likely related even if descriptions differ.

### 3.3 Scoring Model

There are two distinct scoring concepts. They must not be conflated.

#### 3.3.1 Source Scores (per-backend, used for ranking)

Each search backend produces its own score type:

| Backend | Raw Output | Conversion to 0–1 | Name |
|---------|-----------|-------------------|------|
| **Vector (sqlite-vec)** | Cosine distance (0 = identical) | `clamp(1.0 - distance, 0.0, 1.0)` | `vecScore` |
| **FTS (BM25)** | Negative float (more negative = better) | `abs(score) / (1.0 + abs(score))` | `ftsScore` |

Both `vecScore` and `ftsScore` are in the range [0, 1] where 1 = perfect match.
`vecScore` must be clamped defensively in case distance outputs drift outside expected bounds.

#### 3.3.2 Display Similarity (user-facing, used for thresholds)

The **display similarity** is what's shown in the triage comment and compared against
thresholds. It is computed per-item after fusion:

```go
// Each item may appear in vector results, FTS results, or both.
// displaySimilarity is the MAX of its source scores.
displaySimilarity = max(vecScore, ftsScore)
```

**Why max, not RRF?** RRF is a ranking function — it produces relative ordering, not
absolute similarity. An RRF score of 0.03 means nothing to a user. But "92% similar" is
immediately understandable. By taking the max source score, we get a meaningful 0–1 value
that corresponds to actual cosine similarity or BM25 relevance.

**RRF is still used for ordering.** When an item appears in both vector and FTS result
lists, its position in the final output is determined by its RRF rank. But the similarity
percentage shown to the user comes from `displaySimilarity`.

#### 3.3.3 Threshold Application

| Threshold | Applied to | Default | Meaning |
|-----------|-----------|---------|---------|
| `similarity-threshold` | `displaySimilarity` | 0.75 | Minimum to show in "similar items" table |
| `duplicate-threshold` | `displaySimilarity` | 0.92 | Minimum to flag as possible duplicate |

#### 3.3.4 RRF for Ordering

Reciprocal Rank Fusion determines the **display order** of results:

```
rrfScore(item) = Σ 1/(k + rank_in_list + 1)
```

Where `k = 60` (standard constant). Items are sorted by `rrfScore` descending. Items that
appear in both vector and FTS lists get a higher RRF score and float to the top.

**Complete flow:**
1. Run vector search → ranked list with `vecScore` per item
2. Run FTS search → ranked list with `ftsScore` per item
3. Remove self-match hits where `item.ID == currentItemID` (for current issue/PR)
4. Compute `rrfScore` for each unique item across both lists
5. Sort by `rrfScore` descending (this is the display order)
6. Compute `displaySimilarity = max(vecScore, ftsScore)` for each item
7. Filter: drop items where `displaySimilarity < similarity-threshold`
8. Truncate to `max-results`
9. Flag items where `displaySimilarity ≥ duplicate-threshold` as duplicates

### 3.4 Comment Format

```markdown
<!-- triage-bot:v1 -->
### 🔍 Triage Report

> [!WARNING]
> **Possible duplicate** of #123 (95% similar)
>
> Fix authentication timeout on login page

<details><summary>📋 Similar items found (3)</summary>

| # | Title | Similarity | Status |
|---|-------|-----------|--------|
| #123 | Fix authentication timeout on login page | 95% | 🟢 open |
| #89 | Login page hangs after 30 seconds | 82% | ⚫ closed |
| #201 | Auth token expiry not handled | 77% | 🟢 open |

</details>

---
<sub>Generated by triage-bot</sub>
```

**No results behavior:** If no items meet the similarity threshold, **do not post any
comment**. Silent no-op. Don't clutter issues/PRs with "nothing found" messages.

**First run behavior:** On the very first run, the index is empty, so no matches will be
found. The bot indexes the item silently and posts no comment. This is correct behavior.

### 3.5 Concurrency & Locking

Since multiple issues/PRs could be opened simultaneously, the GitHub Action workflow must use:

```yaml
concurrency:
  group: triage-index
  cancel-in-progress: false
```

This ensures only one triage job runs at a time, preventing concurrent writes to the orphan branch. Jobs queue instead of canceling, so no events are lost.

---

## 4. Database Schema

Single SQLite file: `index.db`

### 4.1 Items Table (Metadata)

```sql
CREATE TABLE IF NOT EXISTS items (
    id TEXT PRIMARY KEY,              -- "issue/123" or "pr/456"
    type TEXT NOT NULL,               -- "issue" or "pr"
    number INTEGER NOT NULL,
    title TEXT NOT NULL,
    body TEXT NOT NULL,
    author TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL DEFAULT 'open',
    labels TEXT NOT NULL DEFAULT '[]', -- JSON array
    files TEXT NOT NULL DEFAULT '[]',  -- JSON array (PR only)
    url TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_items_type ON items(type);
CREATE INDEX IF NOT EXISTS idx_items_number ON items(number);
CREATE INDEX IF NOT EXISTS idx_items_state ON items(state);
```

### 4.2 FTS Table (Full-Text Search)

```sql
CREATE VIRTUAL TABLE IF NOT EXISTS items_fts USING fts5(
    title,
    body,
    content='items',
    content_rowid='rowid',
    tokenize='porter unicode61'
);

-- Triggers to keep FTS in sync
CREATE TRIGGER IF NOT EXISTS items_fts_insert AFTER INSERT ON items BEGIN
    INSERT INTO items_fts(rowid, title, body) VALUES (new.rowid, new.title, new.body);
END;

CREATE TRIGGER IF NOT EXISTS items_fts_delete AFTER DELETE ON items BEGIN
    DELETE FROM items_fts WHERE rowid = old.rowid;
END;

CREATE TRIGGER IF NOT EXISTS items_fts_update AFTER UPDATE ON items BEGIN
    DELETE FROM items_fts WHERE rowid = old.rowid;
    INSERT INTO items_fts(rowid, title, body) VALUES (new.rowid, new.title, new.body);
END;
```

### 4.3 Vector Table (sqlite-vec)

```sql
CREATE VIRTUAL TABLE IF NOT EXISTS items_vec USING vec0(
    id TEXT PRIMARY KEY,
    embedding float[1536] distance_metric=cosine
);
```

### 4.4 Schema Versioning

```sql
CREATE TABLE IF NOT EXISTS schema_version (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL
);
```

Check version on startup. Run migrations sequentially if behind.

---

## 5. Project Structure

```
triage-bot/
│
├── cmd/
│   └── triage/
│       └── main.go                      # CLI entry point, parses GitHub event JSON
│
├── internal/
│   ├── engine/
│   │   ├── engine.go                    # Core orchestrator (Handle method)
│   │   └── engine_test.go
│   │
│   ├── ingest/
│   │   ├── issue.go                     # Issue → embeddable text
│   │   ├── pr.go                        # PR → embeddable text (title+body+files+diff)
│   │   ├── diff.go                      # Diff parsing and summarization
│   │   └── ingest_test.go
│   │
│   ├── embed/
│   │   ├── embedder.go                  # Embedder interface definition
│   │   ├── github_models.go             # GitHub Models API implementation
│   │   ├── github_models_test.go
│   │   └── mock.go                      # Mock embedder for tests
│   │
│   ├── store/
│   │   ├── store.go                     # SQLite + sqlite-vec store
│   │   ├── search.go                    # Two-step vector search (QMD pattern)
│   │   ├── fts.go                       # BM25 full-text search
│   │   ├── rrf.go                       # Reciprocal Rank Fusion + display similarity
│   │   ├── migrations.go               # Schema versioning & migrations
│   │   ├── store_test.go
│   │   └── search_test.go
│   │
│   ├── github/
│   │   ├── client.go                    # GitHub API wrapper (go-github/v60+)
│   │   ├── event.go                     # Parse webhook event payloads
│   │   ├── comment.go                   # Post/update triage comments
│   │   ├── state.go                     # Orphan branch state (pull/push index.db)
│   │   └── client_test.go
│   │
│   └── respond/
│       ├── formatter.go                 # Build markdown triage report
│       └── formatter_test.go
│
├── action.yml                           # GitHub Action metadata (composite)
├── .github/
│   └── workflows/
│       └── release.yml                  # CI: build binary, publish GitHub Release
├── go.mod
├── go.sum
├── spec.md                              # This file
└── README.md
```

---

## 6. Component Specifications

### 6.1 CLI Entry Point (`cmd/triage/main.go`)

The binary is invoked by the GitHub Action with environment variables:

```
GITHUB_TOKEN                — Auth token (auto-provided)
GITHUB_EVENT_NAME           — "issues" or "pull_request_target"
GITHUB_EVENT_PATH           — Path to JSON event payload
GITHUB_REPOSITORY           — "owner/repo"

INPUT_SIMILARITY_THRESHOLD  — Optional, default "0.75"
INPUT_DUPLICATE_THRESHOLD   — Optional, default "0.92"
INPUT_MAX_RESULTS           — Optional, default "5"
INPUT_INDEX_BRANCH          — Optional, default "triage-index"
```

> **Note:** GitHub Actions converts input names to environment variables by uppercasing
> and replacing `-` with `_`, then prefixing with `INPUT_`. So `similarity-threshold`
> becomes `INPUT_SIMILARITY_THRESHOLD`.

**Flow:**
1. Parse environment variables and event payload
2. Initialize state manager, pull `index.db` from orphan branch
3. Open SQLite store
4. Initialize GitHub Models embedder
5. Call `engine.Handle(ctx, event)`
6. Push updated `index.db` to orphan branch
7. Exit 0

**Error handling:** All errors are **non-fatal**. If any step fails (embedding API down,
SQLite corruption, GitHub API rate limit), log the error using `::warning::` GitHub Actions
log annotation and **exit 0**. The bot must never block a PR or issue from being opened.
The only operation that should cause exit 1 is a panic (unrecoverable bug).

### 6.2 Engine (`internal/engine/engine.go`)

The core orchestrator. Single method:

```go
func (e *Engine) Handle(ctx context.Context, event Event) error
```

**Event struct:**
```go
type Event struct {
    Type       string   // "issue" or "pr"
    Action     string   // "opened", "edited", "synchronize"
    Owner      string
    Repo       string
    Number     int
    Title      string
    Body       string
    Author     string
    Labels     []string
    State      string   // "open" or "closed"
    URL        string   // HTML URL for linking
    Diff       string   // PR only: unified diff content
    Files      []string // PR only: changed file paths
}
```

**Handle flow:**
1. Build embeddable content string from event (see Section 3.2)
2. Generate embedding via GitHub Models API
3. Compute `currentItemID` (`issue/{number}` or `pr/{number}`)
4. Run vector search (sqlite-vec, two-step pattern), excluding `currentItemID` → `vecResults []ScoredItem`
5. Run FTS search (BM25), excluding `currentItemID` → `ftsResults []ScoredItem`
6. Merge: compute `rrfScore` for ordering, `displaySimilarity` for thresholds (see Section 3.3)
7. Filter by `similarity-threshold`, truncate to `max-results`
8. Flag items where `displaySimilarity ≥ duplicate-threshold` as duplicates
9. Upsert new item into store (metadata + vector + FTS)
10. If matches found: post/update comment on GitHub. If no matches: do nothing.
11. Return nil (or error)

**Idempotency:** If the bot has already commented on the issue/PR (check for existing comment by the bot), update the existing comment instead of posting a new one. Identify bot comments by a hidden HTML marker:
```html
<!-- triage-bot:v1 -->
```

### 6.3 Embedder Interface (`internal/embed/embedder.go`)

```go
type Embedder interface {
    Embed(ctx context.Context, text string) ([]float32, error)
    EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
    Dimensions() int
}
```

**GitHub Models implementation:**
- Endpoint: `https://models.inference.ai.azure.com/embeddings`
- Model: `text-embedding-3-small`
- Dimensions: 1536
- Auth: Bearer token from `GITHUB_TOKEN`
- Rate limiting: Respect `Retry-After` header, exponential backoff (3 retries, 1s/2s/4s)
- Timeout: 30 seconds per request
- Input truncation: Truncate input to 8191 tokens (model limit). Approximate by truncating
  to 30,000 characters (conservative estimate of ~3.7 chars/token).

### 6.4 Store (`internal/store/`)

#### Vector Search (CRITICAL: Two-Step Pattern)

Based on a known bug in sqlite-vec discovered by the QMD project:

**DO NOT combine sqlite-vec virtual table queries with JOINs in a single SQL statement. It will hang indefinitely.** Reference: https://github.com/tobi/qmd/pull/23

```go
// STEP 1: Vector-only query (NO JOINs!)
rows := db.Query(`
    SELECT id, distance
    FROM items_vec
    WHERE embedding MATCH ? AND k = ?
`, vectorBytes, limit*3)

// STEP 2: Separate metadata lookup for each hit
for _, hit := range vectorHits {
    if hit.ID == excludeID {
        continue // never match an item against itself
    }
    row := db.QueryRow(`SELECT ... FROM items WHERE id = ?`, hit.ID)
}
```

**Vector score conversion:**
```go
vecScore := math.Max(0.0, math.Min(1.0, 1.0-distance)) // clamp to [0,1]
```

#### FTS Search

```go
func SearchFTS(db *sql.DB, query string, excludeID string, limit int) ([]FTSResult, error) {
    rows := db.Query(`
        SELECT
            i.id, i.type, i.number, i.title, i.state, i.url,
            bm25(items_fts, 10.0, 1.0) as score
        FROM items_fts f
        JOIN items i ON i.rowid = f.rowid
        WHERE items_fts MATCH ?
          AND i.id != ?
        ORDER BY score ASC
        LIMIT ?
    `, buildFTS5Query(query), excludeID, limit)
    // ...
}
```

**BM25 score conversion:**
```go
ftsScore := math.Abs(rawBM25) / (1.0 + math.Abs(rawBM25))
```

This maps: strong(-10)→0.91, medium(-2)→0.67, weak(-0.5)→0.33, none(0)→0.
Formula is monotonic and query-independent (no per-query normalization needed).

#### Self-Match Exclusion (Required)

Self-match filtering is mandatory, especially on `edited` and `synchronize` events where the
current item is already indexed.

- Compute `currentItemID` as `issue/{number}` or `pr/{number}` from the incoming event
- Exclude `currentItemID` from vector hits in the two-step lookup loop
- Exclude `currentItemID` from FTS SQL (`AND i.id != ?`)
- As a defensive fallback, drop any `item.ID == currentItemID` before fusion

Without this guard, the bot can incorrectly report an issue/PR as a duplicate of itself.

#### FTS Query Building

Convert natural language into FTS5 query syntax:
```go
func buildFTS5Query(input string) string {
    // Split into words, remove stop words, join with implicit AND
    // Example: "fix login timeout" → "fix AND login AND timeout"
    // Escape special FTS5 characters: * " ( ) : ^
}
```

#### Reciprocal Rank Fusion + Display Similarity

```go
type FusedResult struct {
    Item              Item
    RRFScore          float64 // For ordering only, not user-facing
    VecScore          float64 // 0-1 cosine similarity (0 if not in vec results)
    FTSScore          float64 // 0-1 normalized BM25 (0 if not in FTS results)
    DisplaySimilarity float64 // max(VecScore, FTSScore) — user-facing percentage
    IsDuplicate       bool    // displaySimilarity ≥ duplicateThreshold
}

func Fuse(vecResults []ScoredItem, ftsResults []ScoredItem, config Config) []FusedResult {
    const k = 60

    // 1. Build score maps
    vecScores := map[string]float64{}  // id → vecScore
    ftsScores := map[string]float64{}  // id → ftsScore
    rrfScores := map[string]float64{}  // id → rrfScore

    // 2. Compute RRF from rank positions
    for rank, item := range vecResults {
        rrfScores[item.ID] += 1.0 / float64(k + rank + 1)
        vecScores[item.ID] = item.Score
    }
    for rank, item := range ftsResults {
        rrfScores[item.ID] += 1.0 / float64(k + rank + 1)
        ftsScores[item.ID] = item.Score
    }

    // 3. Build fused results
    // Sort by rrfScore descending (display order)
    // Set displaySimilarity = max(vecScore, ftsScore)
    // Set isDuplicate = displaySimilarity >= config.DuplicateThreshold
    // Filter: displaySimilarity >= config.SimilarityThreshold
    // Truncate to config.MaxResults
}
```

### 6.5 GitHub Client (`internal/github/`)

#### State Management (Orphan Branch)

**Pull (download index.db):**
```
1. Create temp directory for git operations
2. git init
3. git remote add origin https://x-access-token:{TOKEN}@github.com/{owner}/{repo}.git
4. git fetch origin {index-branch} --depth=1
5. If fetch fails (branch doesn't exist) → return empty path (first run)
6. git checkout FETCH_HEAD -- index.db
7. Copy index.db to working path
```

**Push (upload index.db):**
```
1. In temp directory:
2. git checkout --orphan {index-branch}
3. git rm -rf . (clean working tree)
4. Copy index.db into temp directory
5. git add index.db
6. git commit -m "Update triage index [skip ci]"
7. git push origin {index-branch} --force
```

The `[skip ci]` in the commit message prevents infinite Action loops.

#### Comment Management

- Use `go-github` to list issue/PR comments
- Find existing bot comment by searching for `<!-- triage-bot:v1 -->` in comment body
- If found: update existing comment (PATCH)
- If not found: create new comment (POST)
- The marker is the first line (hidden in rendered markdown)
- When updating: if the new triage result has no matches, **delete** the existing comment
  (don't leave a stale report)

### 6.6 Response Formatter (`internal/respond/formatter.go`)

Builds the markdown triage comment.

**Structure:**
```
<!-- triage-bot:v1 -->
### 🔍 Triage Report

{duplicate warning if displaySimilarity ≥ duplicateThreshold}

{similar items table if any matches ≥ similarityThreshold}

{footer}
```

**Duplicate warning (GitHub Alert syntax):**
```markdown
> [!WARNING]
> **Possible duplicate** of #123 (95% similar)
>
> {title of duplicate}
```

**Similar items table (collapsible):**
```markdown
<details><summary>📋 Similar items found (3)</summary>

| # | Title | Similarity | Status |
|---|-------|-----------|--------|
| #123 | Fix auth timeout | 95% | 🟢 open |
| #89 | Login hangs | 82% | ⚫ closed |

</details>
```

**Status icons:**
- 🟢 = open
- ⚫ = closed
- 🟣 = merged (PR only)

**Similarity display:** `displaySimilarity` × 100, rounded to integer. Example: 0.9234 → "92%".

---

## 7. Action Definition

### 7.1 `action.yml`

```yaml
name: 'Triage Bot'
description: 'Zero-config duplicate detection for Issues and Pull Requests'
author: 'your-org'

inputs:
  similarity-threshold:
    description: 'Minimum similarity to show as related (0.0 - 1.0)'
    required: false
    default: '0.75'
  duplicate-threshold:
    description: 'Minimum similarity to flag as duplicate (0.0 - 1.0)'
    required: false
    default: '0.92'
  max-results:
    description: 'Maximum number of similar items to show'
    required: false
    default: '5'
  index-branch:
    description: 'Branch name for storing the triage index'
    required: false
    default: 'triage-index'

runs:
  using: 'composite'
  steps:
    - name: Download triage-bot
      shell: bash
      run: |
        set -euo pipefail
        VERSION="v1.0.0"  # immutable release tag
        BINARY="triage-bot-linux-amd64"
        BASE_URL="https://github.com/your-org/triage-bot/releases/download/${VERSION}"

        curl -fsSL "$BASE_URL/$BINARY" -o "/tmp/$BINARY"
        curl -fsSL "$BASE_URL/$BINARY.sha256" -o "/tmp/$BINARY.sha256"
        (cd /tmp && sha256sum -c "$BINARY.sha256")

        mv "/tmp/$BINARY" /tmp/triage-bot
        chmod +x /tmp/triage-bot

    - name: Run triage
      shell: bash
      run: /tmp/triage-bot
      env:
        INPUT_SIMILARITY_THRESHOLD: ${{ inputs.similarity-threshold }}
        INPUT_DUPLICATE_THRESHOLD: ${{ inputs.duplicate-threshold }}
        INPUT_MAX_RESULTS: ${{ inputs.max-results }}
        INPUT_INDEX_BRANCH: ${{ inputs.index-branch }}

branding:
  icon: 'search'
  color: 'blue'
```

### 7.2 Release Build Workflow

```yaml
# .github/workflows/release.yml — in the triage-bot repo itself
name: Release
on:
  push:
    tags: ['v*']

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'

      - name: Install build dependencies
        run: sudo apt-get install -y gcc libsqlite3-dev

      - name: Build
        run: |
          CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
          go build -ldflags="-s -w -extldflags '-static'" \
          -o triage-bot-linux-amd64 ./cmd/triage

      - name: Generate SHA256 checksum
        run: sha256sum triage-bot-linux-amd64 > triage-bot-linux-amd64.sha256

      - name: Release
        uses: softprops/action-gh-release@v1
        with:
          files: |
            triage-bot-linux-amd64
            triage-bot-linux-amd64.sha256
```

---

## 8. Edge Cases & Error Handling

### 8.1 First Run (No Index Exists)
- Orphan branch doesn't exist yet → `git fetch` fails (expected)
- Create empty `index.db` with full schema
- Index the current event (so future events can find it)
- No matches found → no comment posted (silent)
- Create orphan branch on push

### 8.2 Edited Issues / Synchronized PRs
- Event `action` is `"edited"` or `"synchronize"`
- Re-generate embedding with updated content
- Upsert: update metadata + replace vector in store
- If bot previously commented: update comment with new results
- If new results are empty but old comment exists: delete old comment

### 8.3 Very Large Diffs
- PR diffs can be huge (thousands of lines)
- Truncate diff to **4000 characters** for embedding content
- Always include full file path list (it's small and high-signal)
- If GitHub API returns 422 for diff that's too large, fall back to title + body + file paths only (no diff)
- If diff API fails entirely, fall back to title + body only

### 8.4 Rate Limiting
- GitHub Models API: respect `Retry-After`, exponential backoff (3 retries: 1s, 2s, 4s)
- GitHub REST API: check `X-RateLimit-Remaining` header. If < 10, log warning.
- If embedding fails after all retries: log `::warning::`, skip search, still index metadata (without vector), exit 0
- If GitHub comment API fails: log `::warning::`, exit 0

### 8.5 Concurrent Events
- `concurrency.group: triage-index` ensures sequential processing
- `cancel-in-progress: false` means jobs queue, no events are dropped
- No data corruption possible (single writer at a time)
- If the queue gets long (many issues opened at once), each job runs independently — acceptable latency

### 8.6 Fork PRs and Security

**`pull_request_target` security model:**

Because we use `pull_request_target`, the Action runs with the **base repository's**
`GITHUB_TOKEN`, which has write permissions. This is necessary for commenting and pushing
state. However, this means the Action runs trusted code (from the base repo's workflow
definition), not the fork's code.

**Security is maintained because:**
1. The Action only reads the PR's **metadata** (title, body, diff) via the GitHub API —
   it never checks out or executes the fork's code
2. The PR diff is used only as text input for embeddings — it's never executed
3. The binary is downloaded from an **immutable release tag**, not from the PR branch
4. The downloaded binary is verified with SHA256 before execution

**What the Action must NOT do:**
- Never `actions/checkout` the PR branch (would allow code execution from forks)
- Never evaluate PR content as code, templates, or expressions
- Never use PR content in shell commands without proper escaping
- Never execute a downloaded binary unless checksum verification passes

### 8.7 Binary / Non-Text PRs
- Some PRs only change images, binaries, configs
- If diff is empty or binary: embed title + body + file paths only
- Still useful — file path overlap is a strong similarity signal

### 8.8 Closed / Merged Items
- Index closed/merged items — they're valuable for duplicate detection
- Store state in metadata, include in search results
- Show status in triage comment (🟢 open / ⚫ closed / 🟣 merged)
- Do NOT filter them out during search

### 8.9 Bot's Own Comments
- Never index or embed the bot's own triage comments
- When ingesting issue/PR body, the content comes from the event payload (original body), not from comments
- Issue comments are not ingested at all in v1

### 8.10 Empty Title or Body
- Some issues/PRs have empty bodies
- If body is empty, embed title only — it's still useful
- If title is empty (shouldn't happen via GitHub UI, but possible via API), use body only
- If both are empty, skip embedding and search, just index the metadata

### 8.11 SQLite Database Corruption
- If `index.db` from the orphan branch fails to open, log `::warning::` and start fresh
  with a new empty database
- Push the new database to overwrite the corrupted one
- Loss of index data is acceptable — it will rebuild over time as new events arrive

---

## 9. Configuration Defaults

| Parameter | Default | Range | Env Var | Description |
|-----------|---------|-------|---------|-------------|
| `similarity-threshold` | `0.75` | 0.0 - 1.0 | `INPUT_SIMILARITY_THRESHOLD` | Min `displaySimilarity` to show in table |
| `duplicate-threshold` | `0.92` | 0.0 - 1.0 | `INPUT_DUPLICATE_THRESHOLD` | Min `displaySimilarity` to flag duplicate |
| `max-results` | `5` | 1 - 20 | `INPUT_MAX_RESULTS` | Max similar items in comment |
| `index-branch` | `triage-index` | string | `INPUT_INDEX_BRANCH` | Orphan branch for state |
| Embedding model | `text-embedding-3-small` | — | — | Hardcoded |
| Embedding dimensions | `1536` | — | — | Determined by model |
| RRF k constant | `60` | — | — | Standard RRF value |
| FTS tokenizer | `porter unicode61` | — | — | Stemming + unicode |
| Diff truncation | `4000` chars | — | — | Max diff chars for embedding |
| Embedding input limit | `30000` chars | — | — | ~8191 tokens |

---

## 10. Testing Strategy

### 10.1 Unit Tests

Every package has tests. Use `:memory:` SQLite for store tests.

- **`store/`** — Schema creation, upsert, two-step vector search, FTS search, RRF fusion,
  display similarity computation. Specifically test that vector search with the two-step
  pattern returns results (regression test for JOIN hang). Test self-match exclusion
  (`excludeID`) for both vector and FTS paths.
- **`embed/`** — Mock embedder returning deterministic vectors. Test batch embedding.
  Test truncation of long inputs. Test retry logic with mock HTTP server.
- **`ingest/`** — Test content building: issue with body, issue without body, PR with
  diff, PR with large diff (truncation), PR with empty diff, PR with binary files only.
- **`respond/`** — Test markdown output for: no results (empty string), similar only,
  duplicate + similar, single duplicate, multiple duplicates, mixed open/closed/merged
  states. Verify `<!-- triage-bot:v1 -->` marker is always first line.
- **`engine/`** — Integration test with mock embedder and in-memory SQLite. Test full
  flow: ingest → embed → search → respond. Include edited-item scenario where the only
  nearest neighbor is itself and verify no duplicate warning is emitted.
- **`store/rrf.go`** — Test RRF with known ranked lists. Verify ordering is by RRF score
  but displaySimilarity comes from source scores. Test edge: item appears in only one
  list. Test edge: item appears in both lists.

### 10.2 Integration Tests

- Real SQLite file (not `:memory:`) with sqlite-vec extension loaded
- Insert known vectors with known cosine distances
- Verify search returns correct items in correct order
- Verify displaySimilarity values match expected cosine similarities
- Test schema migration from v0 → v1

### 10.3 E2E Test (Manual / CI)

- Create a test GitHub repo
- Open 3 issues with distinct content
- Open a 4th issue that's nearly identical to #1
- Verify bot comments on #4 with duplicate warning referencing #1
- Open a PR that's similar (not duplicate) to #2
- Verify bot comments with similar items table
- Edit #4's title to be completely different
- Verify bot updates its comment (or deletes if no longer similar)

---

## 11. Performance Targets

| Metric | Target | Rationale |
|--------|--------|-----------|
| Total Action runtime | < 30 seconds | User experience; Actions billing |
| Binary download | < 3 seconds | Pre-built ~15MB binary via curl |
| State pull (git fetch) | < 5 seconds | Shallow clone, single file |
| Embedding API call | < 5 seconds | Single request to GitHub Models |
| SQLite search | < 100ms | In-process, small dataset |
| State push (git push) | < 5 seconds | Single file commit |
| Memory usage | < 100MB | Actions runners have 7GB |

For reference: Docker image build from source would add 60-90s, which is why we use
pre-built binaries.

---

## 12. Future Enhancements (Out of Scope for v1)

These are NOT part of the initial build but are designed-for in the architecture:

1. **Batch indexing command** — CLI subcommand to index all existing issues/PRs in a repo
2. **Label suggestions** — Use an LLM to suggest labels based on content analysis
3. **Cross-repo search** — Search across multiple repos in an org
4. **Auto-close duplicates** — Configurable auto-close with reference to original
5. **GHCR container distribution** — Pre-built Docker image for faster cold start
6. **Configurable embedding providers** — Support OpenAI, Gemini, or local models
7. **Webhook server mode** — Run as a persistent GitHub App instead of an Action
8. **Dashboard** — GitHub Pages site showing triage statistics

---

## 13. Reference Implementations

This spec draws architectural patterns from two open-source projects:

### similigh/simili-bot
- **Pipeline pattern** — Modular step-based processing (gatekeeper → search → triage → respond → index)
- **State on orphan branch** — Persistent state via dedicated Git branch using GitHub API
- **Config inheritance** — YAML-based configuration with extends support

### tobi/qmd
- **sqlite-vec for vector search** — Production-proven SQLite-based vector storage
- **Two-step query pattern** — Critical bug avoidance for sqlite-vec JOINs
- **Hybrid search (BM25 + Vector + RRF)** — Combining full-text and semantic search
- **Score normalization** — BM25 scores converted via `|score| / (1 + |score|)`, cosine distance via `1 - distance`

---

## 14. Success Criteria

The v1 is complete when:

- [ ] Dropping the single workflow YAML into any repo triggers triage on new issues and PRs
- [ ] No API keys, config files, or external services required
- [ ] Duplicate detection correctly flags near-identical items (≥92% display similarity)
- [ ] Similar items are surfaced in a clear, collapsible comment with correct percentages
- [ ] Display similarity percentages correspond to actual cosine similarity / BM25 relevance
- [ ] Index persists across Action runs via orphan branch
- [ ] Bot updates its own comment on re-trigger (no duplicate comments)
- [ ] Bot posts no comment when no matches found (silent no-op)
- [ ] Bot never flags an item as a duplicate of itself (self-match exclusion works)
- [ ] Fork PRs are handled securely via `pull_request_target`
- [ ] Downloaded binary is checksum-verified before execution
- [ ] Total Action runtime under 30 seconds for typical repos
- [ ] All unit and integration tests pass
- [ ] Works on both public and private repositories
- [ ] All errors are non-fatal (exit 0 with `::warning::` annotations)
