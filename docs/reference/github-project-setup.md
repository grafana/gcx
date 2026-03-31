# GitHub Project Setup Guide

Sync beads issues to GitHub Issues, set up labels, sub-issue hierarchy,
milestones, and a project board.

## Prerequisites

- `bd` (beads CLI) initialized with issues
- `gh` CLI authenticated (`gh auth status`)
- GitHub repo configured in beads:
  ```bash
  bd config set github.owner <owner>
  bd config set github.repo <repo>
  ```

## Phase 1: Initial Sync

The first sync **must** use `--prefer-local` to establish beads as source of
truth. The default `--prefer-newer` reopens closed issues because freshly-pushed
GitHub issues have newer timestamps.

```bash
# Dry run first
GITHUB_TOKEN="$(gh auth token)" bd github sync --dry-run

# Initial sync — beads wins all conflicts
GITHUB_TOKEN="$(gh auth token)" bd github sync --prefer-local
```

Subsequent syncs use the default:
```bash
GITHUB_TOKEN="$(gh auth token)" bd github sync
```

### Verifying sync state

```bash
# Compare counts
bd list --status=open --json | jq 'length'
gh issue list --repo <owner>/<repo> --state open --limit 500 --json number | jq 'length'

# Spot-check: closed beads should be closed on GitHub
bd list --status=closed --limit=5 --json | jq -r '.[].title'
# Search each title on GitHub — state should be CLOSED
```

After sync, beads stores the GitHub URL in the `external_ref` field:
```bash
bd list --limit=3 --json | jq -r '.[] | "\(.id)\t\(.external_ref)"'
```

## Phase 2: Labels

Set up `priority/` and `area/` labels. Issue types (Bug, Task, Feature, Epic)
are handled natively by GitHub's issue type system.

### Label scheme

| Prefix | Purpose | Colors |
|--------|---------|--------|
| `priority/` | Severity (critical → none) | Red → green gradient |
| `area/` | Codebase area (3-5 max) | Distinct colors |
| `action/` | Workflow state (needs-triage) | Yellow |

> **Note**: Issue *types* (Bug, Task, Feature, Epic) are set via GitHub's
> native issue type system, not labels.

### Remove unused labels

```bash
# GitHub defaults not used by beads
for label in "bug" "documentation" "duplicate" "enhancement" \
             "good first issue" "help wanted" "invalid" "question" "wontfix"; do
  gh label delete "$label" --repo <owner>/<repo> --yes 2>/dev/null &
done
wait

# Find non-standard labels to review
gh label list --repo <owner>/<repo> --json name | jq -r '.[].name' | \
  grep -v -E '^(priority/|area/|action/)'
```

> **Keep labels referenced in `.github/`** (issue templates, dependabot, workflows).
> Check with: `grep -r 'labels' .github/ | grep -v '.md'`

### Colorize beads labels

```bash
# Priority — red→green severity gradient
gh label edit "priority/critical" --repo <owner>/<repo> --color "b60205"
gh label edit "priority/high"     --repo <owner>/<repo> --color "d93f0b"
gh label edit "priority/medium"   --repo <owner>/<repo> --color "fbca04"
gh label edit "priority/low"      --repo <owner>/<repo> --color "c2e0c6"
gh label edit "priority/none"     --repo <owner>/<repo> --color "e4e4e4"
```

### Create area labels

Determine areas from codebase structure and issue clustering (3-5 max).

```bash
gh label create "area/<name>" --repo <owner>/<repo> \
  --color "<hex>" --description "<what it covers>"
```

Color palette: `1d76db` (blue), `5319e7` (purple), `0e8a16` (green),
`e36209` (orange), `b60205` (red).

### Assign area labels

```bash
# Batch by area — parallel is fine for labels
for n in <issue numbers>; do
  gh issue edit $n --repo <owner>/<repo> --add-label "area/<name>" &
done
wait
```

### Update issue templates

Set `type:` in issue template frontmatter to map to GitHub's native issue
types. Remove type labels from `labels:` — they're redundant.

## Phase 3: Sub-Issues (Hierarchy)

GitHub sub-issues mirror beads parent-child hierarchy via the GraphQL API
(no REST or `gh` CLI support).

### Map the hierarchy

```bash
bd list --limit=0 --json | jq -r '.[] | select(.parent) | "\(.id)\t\(.parent)\t\(.title)"'
```

### Get GitHub issue node IDs

```bash
gh api graphql --paginate -f query='
query($endCursor: String) {
  repository(owner: "<owner>", name: "<repo>") {
    issues(first: 100, after: $endCursor) {
      pageInfo { hasNextPage endCursor }
      nodes { id number title }
    }
  }
}' | jq -r '.data.repository.issues.nodes[] | "\(.number)\t\(.id)\t\(.title)"'
```

### Create sub-issue relationships

Match beads parent-child pairs to GitHub node IDs (by title), then:

```bash
gh api graphql -f query='
mutation {
  addSubIssue(input: {
    issueId: "<parent_node_id>"
    subIssueId: "<child_node_id>"
  }) {
    issue { number }
    subIssue { number }
  }
}'
```

Process top-down: epics first, then features, then tasks. Add a small delay
(`sleep 0.3`) between calls to avoid rate limiting.

## Phase 4: Milestones

```bash
# Create milestones with due dates
gh api repos/<owner>/<repo>/milestones \
  -f title="<name>" -f due_on="<YYYY-MM-DDT00:00:00Z>"

# Assign issues to milestones
gh issue edit <number> --repo <owner>/<repo> --milestone "<name>"
```

> **Tip**: Parallel milestone assignment can fail with conflicts. Run
> sequentially or retry failures.

## Phase 5: Project Board

### Create and populate

```bash
gh project create --owner <owner> --title "<name>" --format json
# Note the project number

# Add issues sequentially (parallel causes "temporary conflict" errors)
gh issue list --repo <owner>/<repo> --state open --limit 500 --json number --jq '.[].number' | \
  while read -r n; do
    gh project item-add <project_number> --owner <owner> \
      --url "https://github.com/<owner>/<repo>/issues/$n"
  done
```

### Configure views

View layout configuration (grouping, board vs. table) is not reliably
exposable via `updateProjectV2View` as of this writing — verify current
API support and fall back to the GitHub UI if unavailable:

1. Change layout to **Board** (columns default to Status)
2. **Group by** Labels for area swimlanes
3. Sort by Priority or Parent issue
4. Create additional views as needed (table grouped by Parent issue, etc.)

## Known Limitations

- **`bd github sync` does not sync hierarchy.** Parent-child must be recreated
  via GraphQL after each sync.
- **No status filter on sync.** All issues (open + closed) are synced.
- **First sync timestamp conflict.** Without `--prefer-local`, closed issues
  get reopened because GitHub copies have newer timestamps.
- **Project views may need manual setup.** `updateProjectV2View` support
  varies — verify current API capabilities before falling back to the UI.
- **Beads label drift.** Subsequent syncs may recreate labels that were deleted.
  Monitor with `gh label list` after syncs.
