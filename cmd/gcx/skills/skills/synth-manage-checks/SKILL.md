---
name: synth-manage-checks
description: Use when the user wants to create, update, pull, push, or delete Synthetic Monitoring checks. Trigger on phrases like "create a check", "add a synthetic check", "update check", "pull my SM checks", "push checks", "delete check", or when the user provides a target URL/hostname/domain for monitoring. For check status overview use synth-check-status. For investigating failing checks use synth-investigate-check.
allowed-tools: [gcx, Bash, Read, Write, Edit]
---

# Synthetic Monitoring Check Manager

Manage SM checks using gcx. Experienced operators — no hand-holding.

## Core Principles

1. Use gcx commands; never call Grafana APIs directly (no curl, no HTTP calls)
2. Trust the user's expertise — no explanations of what SM or gcx is
3. Use `-o json` for agent processing; default table format for user display
4. Prefer explicit create/update flows (`create`, `update`) over generic wrappers
5. Probe names are case-sensitive — always copy-paste from `gcx synth probes list`

## Workflow 1: Create New Check

### Step 1: Determine Check Type

Use the decision table in `references/check-types.md`:

| Target | Check Type |
|--------|------------|
| URL (`https://...`, `http://...`) | HTTP |
| Hostname or IP (no port) | Ping |
| Domain name (DNS lookup) | DNS |
| `host:port` | TCP |
| URL with routing path analysis | Traceroute |

If unsure, ask the user what they want to test (availability, DNS, port connectivity, routing).

### Step 2: List and Select Probes

```bash
gcx synth probes list
```

Recommend at least 3 geographically distributed probes. Copy names exactly as shown — case-sensitive. Suggest probes across different continents or regions to provide meaningful coverage (e.g., one each from North America, Europe, Asia-Pacific).

### Step 3: Build YAML Definition

Use the template from `references/check-types.md` for the chosen type. Scaffold the file locally:

```yaml
apiVersion: syntheticmonitoring.ext.grafana.app/v1alpha1
kind: Check
metadata:
  name: <job-name>   # Non-numeric = create; numeric = update
spec:
  job: <job-name>
  target: <target>
  frequency: 60000    # milliseconds; 10000-120000 typical
  timeout: 10000      # milliseconds; must be < frequency
  enabled: true
  labels:
    environment: production
    team: platform
  probes:
    - Atlanta
    - Frankfurt
    - Singapore
  alertSensitivity: medium  # none, low, medium, high
  basicMetricsOnly: false   # true = fewer metrics, lower cardinality
  settings:
    http: {}   # Replace with type-specific settings
```

Configuration guidance:
- **frequency**: critical checks 10,000–60,000ms; standard checks 60,000–300,000ms
- **timeout**: must be strictly less than `frequency`; typically 5,000–30,000ms
- **alertSensitivity**: `high` = alert if >5% failing; `medium` = >10%; `low` = >25%; `none` = no alerts
- **basicMetricsOnly**: `true` reduces metric cardinality (fewer label dimensions); `false` emits full metrics

### Step 4: Create the Check

```bash
# Optional pre-flight validation for HTTP targets
gcx synth checks create -f <file.yaml> --validate-targets

# Create check from manifest
gcx synth checks create -f <file.yaml>
```

Create/update semantics in this repo:
- **Create**: `gcx synth checks create -f <file.yaml>`
- **Update**: `gcx synth checks update <name-or-id> -f <file.yaml>`

After creation, verify with:
```bash
gcx synth checks list
gcx synth checks status <ID>
```

## Workflow 2: Update Existing Check

### Step 1: Pull Current Definition

Fetch the specific check or all checks:
```bash
# Get single check as YAML (use ID or resource name from list output)
gcx synth checks get <ID_OR_NAME> -o yaml > check.yaml

# Export all checks (iterate over list output)
gcx synth checks list -o json
gcx synth checks get <ID_OR_NAME> -o yaml > ./sm-checks/<name>.yaml
```

### Step 2: Edit and Update

Edit the YAML file and modify only the fields that need changing.

```bash
# Optional pre-flight validation for HTTP targets
gcx synth checks update <ID_OR_NAME> -f check.yaml --validate-targets

# Apply update
gcx synth checks update <ID_OR_NAME> -f check.yaml
```

## Workflow 3: GitOps Sync (Pull/Push)

Pull all checks to local directory, edit in source control, push to apply:

```bash
# Pull all checks: list names/IDs, then export each check
gcx synth checks list -o json
gcx synth checks get <ID_OR_NAME> -o yaml > ./sm-checks/<name>.yaml

# Edit files as needed, then apply each update
gcx synth checks update <ID_OR_NAME> -f ./sm-checks/<name>.yaml --validate-targets
gcx synth checks update <ID_OR_NAME> -f ./sm-checks/<name>.yaml
```

For bulk updates, iterate over changed files and update each check explicitly.

## Workflow 4: Delete Checks

```bash
# List checks to confirm IDs
gcx synth checks list

# Delete one or more checks (by numeric ID)
gcx synth checks delete <ID>

# Skip confirmation prompt
gcx synth checks delete <ID> -f

# Delete multiple checks
gcx synth checks delete <ID1> <ID2> <ID3>
```

Confirm the check identity (job name and target) before deleting — use `gcx synth checks get <ID>` to review.

## Output Format

After creating or updating:
```
Check: <job-name>
Target: <target>
Type: <HTTP|Ping|DNS|TCP|Traceroute>
Probes: <count> selected (<list>)
Push: SUCCESS — ID: <assigned-id>

Verify status:
  gcx synth checks status <ID>
```

After pull:
```
Pulled <N> checks to <dir>/
Files: <list of filenames>
```

After delete:
```
Deleted check <ID> (<job-name> -> <target>)
```

## Error Handling

- **"probe not found"**: Probe names are case-sensitive. Run `gcx synth probes list` and copy names exactly.
- **"timeout must be less than frequency"**: Reduce `timeout` value or increase `frequency`.
- **"invalid frequency"**: `frequency` must be between 10,000ms and 120,000ms (10s–2min).
- **Dry-run fails with validation error**: Fix the YAML field indicated in the error before pushing.
- **Push fails with "check already exists"**: The check job+target combination may already exist. Use `gcx synth checks list` to find it and update instead of create.
- **No probes available**: Run `gcx synth probes list`; if empty, verify gcx context and SM API access.
- **Complex check types (MultiHTTP, Browser, Scripted)**: Settings map is not fully documented. Pull an existing check of that type as a template: `gcx synth checks get <ID> -o yaml`.
