# GitHub Project Setup Guide

Set up GitHub Issues sync, sub-issue hierarchy, labels, and a project board
from beads issue state. This guide assumes beads is initialized with issues
and a GitHub repo exists.

## Prerequisites

- `bd` (beads CLI) initialized in the project
- `gh` CLI authenticated (`gh auth status`)
- `GITHUB_TOKEN` available (e.g., `export GITHUB_TOKEN="$(gh auth token)"`)
- GitHub repo configured in beads:
  ```bash
  bd config set github.owner <owner>
  bd config set github.repo <repo>
  ```

## Phase 1: Initial Sync

**Critical**: The first sync MUST use `--prefer-local` to establish beads as
the source of truth. The default `--prefer-newer` will reopen closed issues
because freshly-pushed GitHub issues have newer timestamps.

```bash
# Dry run first
GITHUB_TOKEN="$(gh auth token)" bd github sync --dry-run

# Initial sync — beads wins all conflicts
GITHUB_TOKEN="$(gh auth token)" bd github sync --prefer-local
```

After the initial sync, subsequent runs can use the default:
```bash
GITHUB_TOKEN="$(gh auth token)" bd github sync
```

### Verifying sync state

```bash
# Compare counts
bd list --status=open --json | jq 'length'
gh issue list --repo <owner>/<repo> --state open --json number | jq 'length'

# Check for reopened issues (should match beads closed count)
bd list --status=closed --json | jq -r '.[].title' > /tmp/closed_titles.txt
gh issue list --repo <owner>/<repo> --state open --limit 200 --json number,title \
  | jq -r '.[] | .title' | while read title; do
    grep -qFx "$title" /tmp/closed_titles.txt && echo "REOPENED: $title"
  done
```

## Phase 2: Labels

Beads auto-creates labels with `priority::` and `type::` prefixes but assigns
them the default grey color (`ededed`). Clean up defaults, colorize, and add
`area::` labels.

### Remove unused labels

```bash
# GitHub default labels (not used by beads)
for label in "bug" "documentation" "duplicate" "enhancement" \
             "good first issue" "help wanted" "invalid" "question" "wontfix"; do
  gh label delete "$label" --repo <owner>/<repo> --yes &
done

# Beads freeform labels (replaced by area:: system)
# List all labels first, identify any that aren't priority::/type::/area::
gh label list --repo <owner>/<repo> --json name | jq -r '.[].name' | \
  grep -v '^priority::' | grep -v '^type::' | grep -v '^area::'
# Then delete the ones you don't want
```

### Colorize beads labels

```bash
# Priority — red→green severity gradient
gh label edit "priority::critical" --repo <owner>/<repo> --color "b60205"
gh label edit "priority::high"     --repo <owner>/<repo> --color "d93f0b"
gh label edit "priority::medium"   --repo <owner>/<repo> --color "fbca04"
gh label edit "priority::low"      --repo <owner>/<repo> --color "c2e0c6"
gh label edit "priority::none"     --repo <owner>/<repo> --color "e4e4e4"

# Type — blue family
gh label edit "type::epic"    --repo <owner>/<repo> --color "3E4B9E"
gh label edit "type::feature" --repo <owner>/<repo> --color "0075ca"
gh label edit "type::task"    --repo <owner>/<repo> --color "5DADE2"
gh label edit "type::bug"     --repo <owner>/<repo> --color "d73a4a"
gh label edit "type::chore"   --repo <owner>/<repo> --color "bfdadc"
```

### Create area labels

Determine areas from the codebase structure and task clustering. Common
approach: 3-5 areas max, based on what directories/concerns the issues touch.

```bash
gh label create "area::<name>" --repo <owner>/<repo> \
  --color "<hex>" --description "<what it covers>"
```

Suggested color palette for areas:
- `1d76db` (blue), `5319e7` (purple), `0e8a16` (green),
  `e36209` (orange), `b60205` (red)

### Assign area labels to issues

```bash
# Batch by area
for n in <issue numbers>; do
  gh issue edit $n --repo <owner>/<repo> --add-label "area::<name>" &
done
wait
```

## Phase 3: Sub-Issues (Hierarchy)

GitHub sub-issues mirror beads parent-child hierarchy. This requires the
GraphQL API — there's no REST or `gh` CLI support.

### Step 1: Map the hierarchy from beads

```bash
bd list --limit=0 --json | jq -r '.[] | "\(.id)\t\(.title)\t\(.parent // "none")"'
```

### Step 2: Get GitHub issue node IDs

```bash
gh api graphql -f query='
{
  repository(owner: "<owner>", name: "<repo>") {
    issues(first: 100, states: OPEN) {
      nodes { id number title }
    }
  }
}' | jq -r '.data.repository.issues.nodes[] | "\(.number)\t\(.id)"'
```

### Step 3: Create sub-issue relationships

For each parent→child pair from beads:

```bash
gh api graphql -f query='
mutation {
  addSubIssue(input: {
    issueId: "<parent_node_id>"
    subIssueId: "<child_node_id>"
  }) {
    issue { number title }
    subIssue { number title }
  }
}'
```

The hierarchy can be multi-level (epic → feature → task). Each call adds one
parent→child link. Process top-down: epics first, then features, then tasks.

### Automation pattern

```bash
# Build a mapping of title → node_id
declare -A node_ids
while IFS=$'\t' read -r num id title; do
  node_ids["$num"]="$id"
done < <(gh api graphql -f query='...' | jq -r '...')

# For each parent-child pair from beads hierarchy
add_sub() {
  gh api graphql -f query="mutation {
    addSubIssue(input: {issueId: \"$1\", subIssueId: \"$2\"}) {
      issue { number } subIssue { number }
    }
  }"
}

add_sub "${node_ids[72]}" "${node_ids[88]}"   # epic → feature
add_sub "${node_ids[88]}" "${node_ids[89]}"   # feature → task
# ...
```

## Phase 4: GitHub Project Board

### Create project

```bash
gh project create --owner <owner> --title "<project name>" --format json
# Note the project number from the output
```

### Add issues

```bash
for n in <issue numbers>; do
  gh project item-add <project_number> --owner <owner> \
    --url "https://github.com/<owner>/<repo>/issues/$n"
done
```

**Note**: Adding many items in parallel can cause "temporary conflict" errors.
Add sequentially or in small batches if this happens.

### Configure views (manual — no API)

The GitHub GraphQL API does not expose view configuration mutations
(`updateProjectV2View` does not exist). Configure in the GitHub UI:

1. Open the project at `https://github.com/users/<owner>/projects/<number>`
2. Click **View 1** dropdown → change layout to **Board**
3. Board columns default to Status (Todo / In Progress / Done)
4. Click **Group by** → select **Labels** for area swimlanes
5. Optionally sort by **Priority** or **Parent issue**
6. Create additional views as needed (e.g., table view grouped by Parent issue)

### Available project fields

Query fields to verify what's available:

```bash
gh api graphql -f query='
{
  node(id: "<project_node_id>") {
    ... on ProjectV2 {
      fields(first: 20) {
        nodes {
          ... on ProjectV2Field { id name }
          ... on ProjectV2SingleSelectField { id name options { id name } }
        }
      }
    }
  }
}'
```

Standard fields include: Title, Assignees, Status, Labels, Parent issue,
Sub-issues progress, Linked pull requests, Milestone, Repository, Reviewers.

## Known Limitations

- **bd github sync does not sync hierarchy.** Parent-child relationships must
  be recreated manually via GraphQL after sync. This is tracked upstream at
  steveyegge/beads#2646 (closed, deferred pre-v1.0).

- **bd github sync has no status filter.** It syncs all issues (open + closed).
  There's no `--status=open` flag.

- **First sync timestamp conflict.** Without `--prefer-local`, the default
  `--prefer-newer` strategy will import GitHub versions of closed issues
  (reopening them in beads) because the push just created them with newer
  timestamps.

- **Project views are UI-only.** Board layout, grouping, sorting, and filters
  cannot be configured via API.

- **Beads labels are uncontrolled.** Beads creates freeform labels during sync
  based on issue metadata. After cleanup, a subsequent sync may recreate
  unwanted labels. Monitor with `gh label list` after syncs.
