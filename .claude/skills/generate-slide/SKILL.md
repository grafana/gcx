---
name: generate-slide
description: >
  Regenerate the gcx marketing bento-box slide (slide.html) with verified commands
  from the current codebase. Builds a fresh binary and reflects against the actual
  command tree. Use when the user says "regenerate slide", "update slide",
  "generate slide", or "/generate-slide".
---

# Generate gcx Marketing Slide

Regenerate `slide.html` — a 1440x900 "bento box" marketing slide — so that every
command shown is verified against the live binary and all stats are accurate.

## Prerequisites

- Go toolchain available
- `slide.html` must exist at the repo root

## Workflow

### Stage 1: Build & Reflect

Build a fresh gcx binary and extract the full command catalog and stats.

```bash
# 1. Build
go build -buildvcs=false -o bin/gcx ./cmd/gcx/

# 2. Extract the full flat command catalog (commands + resource_types)
./bin/gcx commands --flat -o json > /tmp/gcx-catalog.json

# 3. Extract counts from the catalog
python3 -c "
import json, sys
d = json.load(open('/tmp/gcx-catalog.json'))
cmds = d.get('commands', [])
rt = d.get('resource_types', [])
leaves = [c for c in cmds if c.get('token_cost')]
print(f'Total commands: {len(cmds)}')
print(f'Leaf commands (with token_cost): {len(leaves)}')
print(f'Resource types: {len(rt)}')
"

# 4. Get provider count
./bin/gcx providers list -o json | python3 -c "
import json, sys
d = json.load(sys.stdin)
print(f'Providers: {len(d)}')
for p in d: print(f'  - {p[\"name\"]}')
"

# 5. Get the human-readable command tree (for selecting showcase commands)
./bin/gcx help-tree --depth 3 -o text
```

### Stage 2: Verify Card Commands

For each card topic, extract the relevant subtree from the binary and confirm
every command shown on the slide actually exists.

```bash
# Per-card verification — run one per card topic:
./bin/gcx help-tree kg --depth 3 -o text           # Card 1: Knowledge Graph
./bin/gcx help-tree assistant --depth 3 -o text     # Card 2: Grafana Assistant
./bin/gcx help-tree fleet --depth 3 -o text         # Card 3: Fleet Management
./bin/gcx help-tree frontend --depth 3 -o text      # Card 4a: App O11y (frontend/faro)
./bin/gcx help-tree appo11y --depth 3 -o text       # Card 4b: App O11y (appo11y)
./bin/gcx help-tree incidents --depth 3 -o text     # Card 5a: Incident Response
./bin/gcx help-tree oncall --depth 3 -o text        # Card 5b: Incident Response (OnCall)
./bin/gcx help-tree synth --depth 3 -o text         # Card 5c: Incident Response (Synth)
./bin/gcx help-tree setup --depth 3 -o text         # Card 6: K8s Observability
./bin/gcx help-tree metrics adaptive --depth 3 -o text  # Card 7a: Adaptive Telemetry
./bin/gcx help-tree logs adaptive --depth 3 -o text     # Card 7b: Adaptive Telemetry
./bin/gcx help-tree traces adaptive --depth 3 -o text   # Card 7c: Adaptive Telemetry
./bin/gcx help-tree slo --depth 3 -o text           # Card 8: Agent-Ready Platform
./bin/gcx help-tree profiles --depth 3 -o text      # Card 8: Agent-Ready Platform
```

For each card, compare the commands currently in `slide.html` against the help-tree
output. Flag any command that does not appear in the tree — it must be replaced.

### Stage 3: Select Showcase Commands

Pick the best commands per card from the help-tree output. Selection criteria:

- **Exists in tree**: The command must appear in the `help-tree` output. No invented commands.
- **Compelling**: Prefer the most impressive capability per product (queries, inspections,
  create/apply operations over plain list commands)
- **Fits width**: Narrow 3fr cards allow ~42 monospace characters. Wide 5fr and 1fr cards
  allow ~80 characters including inline comments.
- **Diverse verbs**: Vary the actions across cards (list, get, create, query, inspect,
  status, apply, show) — avoid walls of `list` commands.
- **Flag accuracy**: Only use flags that appear in the help-tree output for that command.

### Stage 4: Update slide.html

Update `slide.html` preserving the exact layout and styling. Only modify HTML content
inside card elements and the stats bar — never touch CSS or grid structure.

#### Layout

```
Header:  [gcx logo]  [tagline]                          [gcx auth login badge]

Row 1:   [ Card 1: Knowledge Graph (1fr)     ] [ Card 2: Grafana Assistant (1fr)   ]

Row 2:   [ Card 3: Fleet Mgmt (5fr)  ] [ Card 4: App O11y (3fr) ] [ Card 5: IRM (3fr) ]

Row 3:   [ Card 6: K8s O11y (5fr)    ] [ Card 7: Adaptive (3fr) ] [ Card 8: Agent (3fr)]

Stats:   [ 1 binary ] [ N+ resource types ] [ N+ commands ] [ N products ]
```

#### Card Topic Mapping

| Card | Provider(s) to query | Badge |
|------|---------------------|-------|
| 1. Knowledge Graph | `kg` | badge-green "Dependencies & Health" |
| 2. Grafana Assistant | `assistant`, `auth` | badge-orange "AI-Powered" |
| 3. Fleet Management | `fleet` | badge-green "Infrastructure" |
| 4. App O11y | `frontend` (faro), `appo11y` | badge-green "End to End Observability" |
| 5. Incident Response | `incidents`, `oncall`, `synth`, `k6` | badge-filled-orange "IRM" |
| 6. K8s Observability | `setup instrumentation`, `fleet` | badge-green "Kubernetes" |
| 7. Adaptive Telemetry | `metrics adaptive`, `logs adaptive`, `traces adaptive` | badge-orange "Cost Control" |
| 8. Agent-Ready Platform | `slo`, `profiles`, `resources`, `assistant` | badge-filled-orange "A2A" |

#### Styling Reference

- Body: `#0b0c0e`, Cards: `#14161a` bg / `#1e2127` border / 10px radius
- Code blocks: `#0e1014` bg / `#2a2d33` left-border / JetBrains Mono 12.5px / line-height 1.7
- Syntax highlighting spans:
  - `.c-bin` (`#6E9FFF`): the `gcx` binary name
  - `.c-cmd` (`#c8ccd0`): provider/group name
  - `.c-kw` (`#c8ccd0`, weight 500): action/subcommand
  - `.c-flag` (`#6E9FFF`): flags like `--type`
  - `.c-val` (`#ff9830`): values and strings
  - `.c-cmt` (`#4a4f57`): inline comments
- Code line format: `<div class="line"><span class="c-bin">gcx</span> <span class="c-cmd">provider</span> <span class="c-kw">action</span> ...</div>`
- Bullet lists use `<ul class="feature-list">` with gradient `>` markers

#### Stats Bar

Update with exact counts from Stage 1:
- `1` binary (always)
- Leaf command count rounded down to nearest 50, with `+` suffix (e.g. `250+`)
- Resource type count rounded down to nearest 5, with `+` suffix (e.g. `60+`)
- Exact provider count from `gcx providers list`

### Stage 5: Verify

1. Open `slide.html` in a browser: `open slide.html`
2. Check at 1440x900 viewport — no text overflow, no layout breaks
3. Confirm narrow cards (3fr) don't have lines wrapping or clipping

## Guard Rails

- Build fresh before every regeneration — never rely on stale data
- Every command on the slide must appear in `gcx help-tree` output
- Every flag on the slide must appear in the help-tree output for that specific command
- Keep inline comments short in narrow cards — omit if they'd cause overflow
- Preserve CSS/styling — only modify HTML content
- Do not change the grid structure, card count, or row ratios
