---
name: synth-manage-checks
description: Creates, updates, exports, and deletes Synthetic Monitoring checks from YAML definitions via gcx. Use when the user wants to create, update, pull, push, or delete Synthetic Monitoring checks. Trigger on phrases like "create a check", "add a synthetic check", "update check", "pull my SM checks", "push checks", "delete check", or when the user provides a target URL/hostname/domain for monitoring. For check status overview use synth-check-status. For investigating failing checks use synth-investigate-check.
allowed-tools: Bash, Read, Write, Edit
---

# Synthetic Monitoring Check Manager

Manage Synthetic Monitoring checks using gcx.

## Core Principles

1. Use gcx commands; never call Grafana APIs directly (no curl, no HTTP calls)
2. Trust the user's expertise — no explanations of what SM or gcx is
3. Use `-o json` for agent processing; default table format for user display
4. gcx validates specs client-side before calling the API (probe names, check type, target format) — no separate dry-run step is needed
5. Probe names are case-sensitive — always copy-paste from `gcx synthetic-monitoring probes list`

## Workflow 1: Create New Check

### Step 1: Determine Check Type

Pick the type from the target format (full decision tree and per-type templates: [check-types.md](references/check-types.md)):

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
gcx synthetic-monitoring probes list
```

Recommend at least 3 geographically distributed probes. Copy names exactly as shown — case-sensitive. Suggest probes across different continents or regions to provide meaningful coverage (e.g., one each from North America, Europe, Asia-Pacific).

### Step 3: Build YAML Definition

Use the template from [check-types.md](references/check-types.md) for the chosen type, filling the type-specific `settings` block. Common fields:

```yaml
apiVersion: syntheticmonitoring.ext.grafana.app/v1alpha1
kind: Check
metadata:
  name: <job-name>   # gcx rewrites this to <job>-<id> after create
spec:
  job: <job-name>
  target: <target>
  frequency: 60000    # milliseconds; 10000-120000 typical
  timeout: 10000      # milliseconds; must be < frequency
  enabled: true
  labels:
    - name: environment
      value: production
    - name: team
      value: platform
  probes:
    - Atlanta
    - Frankfurt
    - Singapore
  alertSensitivity: none    # none unless the stack uses legacy sensitivity alerts
  basicMetricsOnly: false   # true = fewer metrics, lower cardinality
  settings:
    http: {}   # Replace with type-specific settings
```

Configuration guidance:
- **frequency**: critical checks 10,000–60,000ms; standard checks 60,000–300,000ms
- **timeout**: must be strictly less than `frequency`; typically 5,000–30,000ms
- **alertSensitivity**: default to `none` — see "Alert Sensitivity" in
  [references/check-types.md](references/check-types.md) for why non-`none`
  values can 403 on some stacks.
- **basicMetricsOnly**: `true` reduces metric cardinality (fewer label dimensions); `false` emits full metrics

### Step 4: Create the Check

```bash
# Create from file
gcx synthetic-monitoring checks create -f <file.yaml>
```

For HTTP checks, `--validate-targets` pre-flights the target with a HEAD request (warning only). If client-side validation fails, fix the field named in the error and re-run.

After creation, verify with:
```bash
gcx synthetic-monitoring checks list
gcx synthetic-monitoring checks status <ID>
```

## Workflow 2: Update Existing Check

### Step 1: Pull Current Definition

Fetch the specific check:
```bash
# Get single check as YAML (use ID from list output)
gcx synthetic-monitoring checks get <ID> -o yaml > check-<ID>.yaml
```

### Step 2: Edit and Update

Edit the YAML file, keeping `metadata.name` unchanged (it is the `<job>-<id>` composite that targets the right check). Modify only the fields that need changing.

```bash
# Update the check from file
gcx synthetic-monitoring checks update <ID> -f check-<ID>.yaml
```

## Workflow 3: GitOps Sync (Pull/Push)

When users say "pull" they mean export with `checks get -o yaml`; "push" means apply with `checks create`/`checks update -f`. Export to files, edit in source control, then update to apply:

```bash
# List all checks and export each to YAML
gcx synthetic-monitoring checks list -o yaml

# Get a specific check as YAML
gcx synthetic-monitoring checks get <ID> -o yaml > ./sm-checks/check-<ID>.yaml

# Edit files as needed, then update each changed file
gcx synthetic-monitoring checks update <ID> -f ./sm-checks/check-<ID>.yaml
```

For bulk updates, update files individually to control which checks are changed.

## Workflow 4: Delete Checks

```bash
# List checks to confirm IDs
gcx synthetic-monitoring checks list

# Delete one or more checks (by numeric ID)
gcx synthetic-monitoring checks delete <ID>

# Skip confirmation prompt
gcx synthetic-monitoring checks delete <ID> --force

# Delete multiple checks
gcx synthetic-monitoring checks delete <ID1> <ID2> <ID3>
```

Confirm the check identity (job name and target) before deleting — use `gcx synthetic-monitoring checks get <ID>` to review.

## Output Format

After creating or updating:
```
Check: <job-name>
Target: <target>
Type: <HTTP|Ping|DNS|TCP|Traceroute>
Probes: <count> selected (<list>)
Result: <created (id=<ID>) | updated>

Verify status:
  gcx synthetic-monitoring checks status <ID>
```

After export:
```
Exported <N> checks to <dir>/
Files: <list of filenames>
```

After delete:
```
Deleted check <ID> (<job-name> -> <target>)
```

## Error Handling

- **"probe not found"**: Probe names are case-sensitive. Run `gcx synthetic-monitoring probes list` and copy names exactly.
- **"timeout must be less than frequency"**: Reduce `timeout` value or increase `frequency`.
- **"invalid frequency"**: The allowed `frequency` range depends on check type (e.g. traceroute allows longer intervals); the API error states the valid range.
- **"check validation failed"**: gcx validates the spec client-side before calling the API. Fix the YAML field indicated in the error and re-run.
- **Create fails with "check already exists"**: The check job+target combination may already exist. Use `gcx synthetic-monitoring checks list` to find it and update instead of create.
- **No probes available**: Run `gcx synthetic-monitoring probes list`; if empty, verify gcx context and SM API access.
- **Complex check types (MultiHTTP, Browser, Scripted)**: Settings map is not fully documented. Pull an existing check of that type as a template: `gcx synthetic-monitoring checks get <ID> -o yaml`.
