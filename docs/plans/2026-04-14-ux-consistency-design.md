# Design: gcx UX Consistency

> **Date**: 2026-04-14
> **Status**: draft
> **Inputs**:
> - [01-onboarding-journeys-report.md](../research/2026-04-10-ux-consistency/01-onboarding-journeys-report.md)
> - [02-setup-framework.md](../research/2026-04-10-ux-consistency/02-setup-framework.md)
> - [03-command-categories-findings.md](../research/2026-04-10-ux-consistency/03-command-categories-findings.md)
> - [Setup framework design](../research/2026-04-10-ux-consistency/2026-04-14-setup-framework-design.md)
> **Related issues**: #387 (verb taxonomy), #287 (unified setup framework)

## Problem

gcx has grown to 16+ providers with ~150 leaf commands across 27 top-level
areas. The CLI surface evolved organically and now has inconsistencies across
multiple dimensions:

1. **No onboarding flow** — users face 6-8 manual steps before first useful
   output. No first-run detection, no wizard, no "start here."
2. **Flat command list** — `gcx --help` shows 27 commands alphabetically
   with no grouping, no progressive disclosure.
3. **Auth/config/setup overlap** — `gcx auth`, `gcx config`, and `gcx setup`
   have unclear boundaries. Auth is conceptually part of config but lives
   separately. `setup` sounds like `init`.
4. **Verb inconsistencies** — `show` vs `get` overlap, sub-resource patterns
   vary, signal provider conventions aren't codified.
5. **Output gaps** — missing `wide` codecs, inconsistent mutation summaries,
   unstructured error output in agent mode.

This design addresses dimensions 1-3. Dimensions 4-5 (verb taxonomy, output
consistency) are covered by research docs 04-06 and will produce separate
implementation plans.

---

## D1: Auth, Config, and Onboarding

### Mental Model

Three user-facing concerns, cleanly separated:

```
USER JOURNEY                    COMMAND
─────────────                   ───────
"Get connected"          →      gcx login
"Get products working"   →      gcx setup
"Tweak settings later"   →      gcx config
```

### `gcx login` — First-Run Wizard

Replaces the current 6-8 step manual dance (`config set` x3 + `auth login` +
`config use-context` + `config check`) with a single guided command.

**Multi-context support:** Each `gcx login` invocation creates (or updates)
a named context. Running login multiple times with different endpoints
produces separate contexts:

```
gcx login --server https://dev.grafana.net     → context "dev"
gcx login --server https://ops.grafana.net     → context "ops"
gcx login --server https://grafana.local:3000  → context "local"
```

Context name is auto-derived from the server hostname (subdomain or host).
Override with `--context NAME`.

**Two-step auth for Grafana Cloud:** Cloud targets require both a
primary auth method (Grafana API) and a Cloud Access Policy token
(Cloud-specific APIs like SM, adaptive telemetry, GCOM). On-prem
targets only need the primary auth. Long-term, OAuth will cover
everything and the CAP step will go away.

| Target | Step 1: Grafana auth | Step 2: Cloud API auth |
|--------|---------------------|----------------------|
| Grafana Cloud (`*.grafana.net`) | OAuth (browser) or SA token | Cloud Access Policy token |
| Grafana on-prem / OSS | SA token only | N/A |

Detection is based on the server URL. If detection is ambiguous (e.g.,
custom domain pointing to Grafana Cloud), the flow falls back to asking
the user.

**Interactive flow — new context:**

```
$ gcx login

  Grafana server URL: https://mystack.grafana.net
  Detected: Grafana Cloud

  Step 1: Grafana authentication
  [1] Browser login (OAuth — recommended for personal use)
  [2] Service Account token (recommended for CI/teams)
  → (authenticates)

  Step 2: Cloud API access
  Some Cloud features (Synthetic Monitoring, Adaptive Telemetry, etc.)
  require a Cloud Access Policy token.
  Paste your Cloud Access Policy token (or Enter to skip): glc_xxx

  ✔ Grafana auth: valid
  ✔ Cloud API auth: valid
  ✔ Context 'mystack' created and set as current

  What's next:
    gcx setup status             View product status
    gcx resources get dashboards List your dashboards
    gcx metrics query 'up'       Query Prometheus
```

**Re-authentication — existing context:**

```
$ gcx login --context mystack

  Context 'mystack' found (https://mystack.grafana.net)

  Step 1: Grafana authentication
  Current: OAuth (expired)
  [1] Re-authenticate via browser (OAuth)
  [2] Switch to Service Account token
  → (re-authenticates)

  Step 2: Cloud API access
  Current: Cloud Access Policy token (valid)
  Keep current token? [Y/n]

  ✔ Grafana auth: refreshed
  ✔ Cloud API auth: unchanged
  ✔ Context 'mystack' updated
```

**On-prem flow (single step):**

```
$ gcx login --server https://grafana.local:3000

  Detected: Grafana (self-hosted)

  Paste your Service Account token: glsa_xxx

  ✔ Authentication: valid
  ✔ Context 'local' created and set as current
```

**Non-interactive path (CI/agents):**

```bash
# New context with both tokens:
gcx login --server https://dev.grafana.net --token glsa_xxx --cloud-token glc_xxx --context dev
# Re-auth existing context:
gcx login --context prod --token glsa_xxx
# Via env vars:
GRAFANA_SERVER=... GRAFANA_TOKEN=... GRAFANA_CLOUD_TOKEN=... gcx login --yes
# Agents don't need login — env vars are sufficient for direct commands
```

**Invocation modes:**

```
gcx login                          # New context (prompted) or re-auth current
gcx login --context prod           # Re-auth existing context
gcx login --server https://new...  # New/update context for that server
```

**Properties:**
- Multi-context — each login creates/updates a named context, sets it as current
- Two-step auth for Cloud (Grafana auth + Cloud API token)
- Auto-detects auth methods from server URL (Cloud vs on-prem)
- Falls back to user prompt when detection is ambiguous
- Re-auth via `--context` — updates expired tokens without re-creating
- Idempotent — safe to re-run; detects existing context, offers to update
- Validates connectivity + discovers stack capabilities
- Prints contextual next-step suggestions on completion
- All prompts have flag equivalents (`--server`, `--token`, `--cloud-token`,
  `--context`, `--yes`)

### `gcx config` — Advanced Configuration

Low-level config manipulation. Power user territory. Moved from "Getting
Started" to "Advanced" in the help grouping.

```
gcx config view              # Show current config (redacted secrets)
gcx config set KEY VALUE     # Set a config property
gcx config edit              # Open config in $EDITOR
gcx config use-context NAME  # Switch active context
gcx config list-contexts     # Show all contexts
gcx config current-context   # Print active context name
gcx config check             # Validate connectivity
gcx config path              # Print config file location
```

### `gcx auth` — Removed as Top-Level Command

Auth is absorbed into `gcx login` (primary auth flow) and `gcx config`
(token management). The narrow auth operations that survive:

| Current | New location | Rationale |
|---------|-------------|-----------|
| `gcx auth login` | `gcx login` | Primary entry point |
| Token revocation | `gcx config set contexts.X.grafana.token ""` | Rare operation, config primitive suffices |
| Token display | `gcx config view` (already shows redacted) | No separate command needed |

If dedicated token management commands prove necessary later, they nest
under `gcx config` (e.g., `gcx config token revoke`), not as a separate
top-level area.

---

## D2: Setup Framework

### `gcx setup` — Product Onboarding & Status Dashboard

**Bare command = `gcx setup status` (status dashboard):**

```
$ gcx setup status

PRODUCT              STATE            DETAILS                NEXT STEP
Instrumentation      active           2 clusters, 14 apps   -
Synthetic Monitoring not configured   -                      gcx synth setup --url <target>
SLOs                 active           5 definitions          -
OnCall               configured       1 integration          gcx oncall setup
Alerting             active           12 rules               -
Frontend             not configured   -                      gcx frontend setup --name <app>
```

**ProductState enum:**

| State | Meaning |
|-------|---------|
| `not_configured` | Product hasn't been set up — no credentials, no resources |
| `configured` | Credentials/config exist but no active resources (e.g., SM token set but no checks created) |
| `active` | Configured AND has active resources producing data |
| `error` | Configured but something is broken (e.g., expired token, unreachable API) |

**Guided flow = `gcx setup init`:**

Infrastructure-oriented, not product-oriented. Asks "what do you have?",
not "which Grafana product?":

```
$ gcx setup init

  What are you working with? (select all that apply)
  [ ] A Kubernetes cluster
  [ ] Linux/VM hosts
  [ ] A web application (browser-side)
  [ ] A public URL to monitor
  [ ] Cloud provider infrastructure (AWS/GCP/Azure)
  [ ] A database to monitor
```

Each selection maps to provider setup commands with parameters collected
during the interactive flow. Users can exit at any point (Ctrl-C) and
gcx will set up whatever has been entered so far. Re-running `setup init`
fills in the gaps — already-configured products are skipped.

Auto-detection of infrastructure (e.g., scanning the repo for Dockerfiles
or k8s manifests) is intentionally out of scope — that's the domain of
AI agents wielding gcx, not the CLI itself.

### Setup as a Verb on Providers

Each provider owns its setup command:

```
gcx synth setup --url https://example.com
gcx frontend setup --name my-app
gcx oncall setup --integration-type alerting
```

Interactive logic lives only in the orchestrator (`gcx setup init`).
Provider setup commands are non-interactive (flags-only). Agents call
provider setup directly, skipping the orchestrator.

### Provider Interfaces

```go
// Setupable — provider has a setup command.
type Setupable interface {
    SetupCommand() *cobra.Command
}

// StatusDetectable — provider can report configuration status.
type StatusDetectable interface {
    Status(ctx context.Context, cfg *config.Config) (*ProductStatus, error)
}

type ProductStatus struct {
    Product   string       // "Synthetic Monitoring"
    State     ProductState // not_configured | configured | active | error
    Details   string       // "3 checks, 2 healthy"
    SetupHint string       // "gcx synth setup --url <target>"
}
```

### Setup Commands by Journey Phase

Mapped to the command groups (D3), following the natural flow: collect
→ understand → operate.

**Signals & Data (collect):**

| Provider | Setup command | Value |
|----------|--------------|-------|
| `synth` | `gcx synth setup --url <target>` | SM credential auto-config — biggest onboarding friction |
| `instrumentation` | `gcx instrumentation setup <cluster>` | K8s + app telemetry via Instrumentation Hub |
| `frontend` | `gcx frontend setup --name <app>` | Create Faro app + output SDK snippet |

**IR & Reliability (operate):**

| Provider | Setup command | Value |
|----------|--------------|-------|
| `slo` | `gcx slo setup --datasource-uid X` | Interactive SLO builder |
| `oncall` | `gcx oncall setup` | Guided integration → escalation → schedule |
| `alert` | `gcx alert setup` | Contact point → policy → first rule |

### Instrumentation Promoted to Top-Level Provider

Instrumentation is a product, not a sub-resource of setup:

```
gcx instrumentation setup <cluster>     # Bootstrap + discovery + enable
gcx instrumentation discover <cluster>  # Find workloads
gcx instrumentation status              # Cluster overview
gcx instrumentation get <cluster>       # Export manifest
gcx instrumentation apply               # Import manifest
gcx instrumentation check <cluster>     # Preflight validation
```

**Breaking change**: `gcx setup instrumentation` → `gcx instrumentation`.
Acceptable in preview mode.

---

## D3: Command Grouping

### Implementation: Cobra `AddGroup()` API

Visual grouping only — zero structural changes. Same 27+ commands, same
invocation syntax, same tab completion, same agent discovery. Only `--help`
rendering changes.

### The Groups (7)

Ordered as a maturity journey: connect → ingest → understand → respond →
codify → customize → learn.

```
Getting Started:
  login               Authenticate and connect to Grafana
  setup               Onboard products and view status

Signals & Data:
  connections         Host/VM integrations (Alloy)
  datasources         Datasource management and queries
  fleet               Fleet Management (pipelines, collectors)
  instrumentation     Instrumentation Hub (K8s + app telemetry)
  logs                Query Loki + Adaptive Logs
  metrics             Query Prometheus + Adaptive Metrics
  profiles            Query Pyroscope + Adaptive Profiles
  traces              Query Tempo + Adaptive Traces

Monitoring & Insights:
  appo11y             App Observability settings
  assistant           AI-powered investigation
  dashboards          Dashboard snapshots
  frontend            Frontend Observability (Faro)
  kg                  Knowledge Graph (Asserts)
  sigil               AI observability (Sigil)

Incident Response & Reliability:
  alert               Alert rules and groups
  incidents           Incident management (IRM)
  k6                  Load testing (K6 Cloud)
  oncall              On-call schedules and escalation
  slo                 Service Level Objectives
  synth               Synthetic Monitoring checks and probes

Observability as Code:
  dev                 Scaffold, import, generate, lint, serve
  resources           Manage Grafana resources (push/pull/get)

Advanced:
  api                 Raw API passthrough
  config              Configuration management
  providers           List registered providers

Help:
  commands            Command catalog (for agents)
  completion          Shell completion
  help                Help about any command
  help-tree           Command tree (for agents)
```

### Rationale

| Group | Function | Mental model |
|-------|----------|-------------|
| **Getting Started** (2) | Auth + onboarding | "Connect me" |
| **Signals & Data** (8) | Ingest telemetry + query it + manage adaptive | "Get data in, query it" |
| **Monitoring & Insights** (6) | Dashboards, topology, AI, frontend SDK | "Understand what's happening" |
| **IR & Reliability** (6) | Alerting, incidents, on-call, SLOs, synthetic, load tests | "Respond and test" |
| **O11y as Code** (2) | GitOps pipeline + dev tooling | "Codify" |
| **Advanced** (3) | Config, raw API, provider introspection | "Customize" |
| **Help** (4) | Help text, agent catalogs, completion | "Learn" |

### Before/After `--help`

**Before:**
```
Available Commands:
  alert        ...
  api          ...
  appo11y      ...
  [24 more commands in alphabetical soup]
```

**After:**
```
Getting Started:
  login             Authenticate and connect to Grafana
  setup             Onboard products and view status

Signals & Data:
  connections       Host/VM integrations (Alloy)
  datasources       Datasource management and queries
  fleet             Fleet Management (pipelines, collectors)
  instrumentation   Instrumentation Hub (K8s + app telemetry)
  logs              Query Loki + Adaptive Logs
  metrics           Query Prometheus + Adaptive Metrics
  profiles          Query Pyroscope + Adaptive Profiles
  traces            Query Tempo + Adaptive Traces

Monitoring & Insights:
  appo11y           App Observability settings
  assistant         AI-powered investigation
  dashboards        Dashboard snapshots
  frontend          Frontend Observability (Faro)
  kg                Knowledge Graph (Asserts)
  sigil             AI observability (Sigil)

Incident Response & Reliability:
  alert             Alert rules and groups
  incidents         Incident management (IRM)
  k6                Load testing (K6 Cloud)
  oncall            On-call schedules and escalation
  slo               Service Level Objectives
  synth             Synthetic Monitoring checks and probes

Observability as Code:
  dev               Scaffold, import, generate, lint, serve
  resources         Manage Grafana resources (push/pull/get)

Advanced:
  api               Raw API passthrough
  config            Configuration management
  providers         List registered providers

Help:
  commands          Command catalog (for agents)
  completion        Shell completion
  help              Help about any command
  help-tree         Command tree (for agents)
```

---

## D4: First-Run Detection

When gcx is run with no arguments and config has no configured contexts:

```
$ gcx

Welcome to gcx — a unified CLI for Grafana Cloud.

Get started:
  gcx login           Set up authentication and connect to your stack
  gcx --help          See all available commands

Already have a config?
  gcx config check    Verify your connection
```

Detection: check if `contexts` in config has only the empty `default` entry.

---

## D5: Post-Command Suggestions

After successful operations, print contextual next-step suggestions:

**After `gcx login`:**
```
What's next:
  gcx setup                    View product status
  gcx resources get dashboards List your dashboards
  gcx metrics query 'up'       Query Prometheus
```

**After `gcx resources pull`:**
```
Pulled 47 resources to ./resources/

What's next:
  gcx resources push --context staging   Promote to staging
  gcx dev lint run ./resources           Lint for best practices
  gcx resources validate -p ./resources  Validate against API schemas
```

**After empty results:**
```
No dashboards found.

Try:
  gcx resources push -p ./dashboards    Push local dashboards
  gcx dev scaffold                      Start a new project
```

Suggestions go to stderr (diagnostic, not data). Suppressed in agent mode
and when stdout is piped.

---

## D6: Verb Taxonomy Refinements

Research doc [04](../research/2026-04-10-ux-consistency/04-verb-taxonomy-findings.md)
audited all 248 leaf commands and found 17 inconsistencies (3 P0) and
31 commands missing `wide` codecs.

### `get` is allowed on non-adapter resources

The CONSTITUTION rule "provider-only resources must not mimic adapter
verbs" was over-broad. `get` for "fetch this thing by ID" is universal
and natural. The real constraint: don't use `list`/`get` if the resource
behaves differently via `resources get` than via the provider command.

`traces get TRACE_ID` is correct as-is — no rename needed.

### Read-only resources are removed from the adapter registry

Resources that can't round-trip through `push`/`pull` don't belong in
the adapter pipeline. If a user runs `resources pull` and gets YAML they
can never push back, that's a UX trap.

**Affected resources:**

| Provider | Resource | Current | New |
|----------|----------|---------|-----|
| alert | rules, groups | Adapter (list/get only) | Provider-only |
| kg | rules, datasets, vendors, entity-types, scopes | Adapter (list/get only) | Provider-only |

These resources remain accessible via their provider commands
(`gcx alert rules list -o yaml`) for export/documentation. They just
disappear from `resources pull/get/push`.

Resources with partial write support (e.g., `appo11y` with update-only)
are evaluated case-by-case. If they support idempotent push (update =
create-or-update), they stay. If push would fail on first run (no create),
they're removed.

### Singleton resources use `show`, not `get`

Resources with no ID and no list (e.g., `appo11y overrides`, `appo11y
settings`) use `show` to distinguish from the adapter `get` which implies
"by ID." This is codified as the **singleton rule** in the verb taxonomy.

### New verb taxonomy rules

Three rules added to `docs/design/naming.md`:

1. **Singleton rule**: Resources with no ID and no list use `show`
2. **`open` disambiguation**: `browse` for browser navigation, explicit
   state verbs (`activate`/`close`) for lifecycle changes
3. **`show`/`list`/`get` decision tree**: `get` = by ID (adapter or not),
   `list` = collection, `show` = singleton or aggregate view

---

## D7: Output Consistency Fixes

Research doc [05](../research/2026-04-10-ux-consistency/05-output-consistency-findings.md)
found systematic gaps across providers.

### Key fixes

- **OnCall `get` subcommand**: outputs raw domain type instead of K8s
  envelope — affects all 15+ OnCall resource types. Fix: align with
  `newListSubcommand` which correctly wraps.
- **k6**: 4 resource types bypass TypedCRUD (factories exist but go
  unused in command layer). Fix: wire commands through TypedCRUD.
- **Delete prompts on stdout**: synth checks, synth probes, slo
  definitions, slo reports write confirmation prompts to stdout instead
  of stderr. Fix: use `cmd.ErrOrStderr()`.
- **31 commands missing `wide` codec**: prioritized by provider (k6: 7,
  oncall: 6, sigil: 5, kg: 4, alert: 3).

---

## D8: Signal Provider Consistency

Research doc [06](../research/2026-04-10-ux-consistency/06-signal-interactions-findings.md)
confirmed PR #348's standardization held for core flags but found 3 bugs
and structural asymmetries.

### Bugs to fix

- `--step` silently ignored on `traces query` and `profiles query` (flag
  accepted, does nothing — remove or wire it)
- `-o wide` on `profiles query` crashes (missing codec case)

### Asymmetries to document (not fix)

- Adaptive sub-trees diverge intentionally (different backend APIs) —
  document this in `docs/design/` as the canonical signal provider pattern
- `metrics` command name carries 3 semantics across signals (LogQL metric
  queries, TraceQL metrics, Pyroscope SelectSeries) — confusing but
  reflects backend terminology. Document the mapping.
- `--limit` defaults differ (logs: 50, traces: 20) — normalize to a
  shared default or document the rationale

---

## D9: Behavioral Contracts

Cross-cutting contracts that standardize how commands of each type behave,
regardless of which provider they belong to. Derived from auditing all
~150 leaf commands for column patterns, output shapes, and verb semantics.

### Contract 1: CRUD Table Columns

Every `list` and `get` command for a CRUD resource follows a canonical
column order:

```
Table:  NAME  [resource-specific columns...]  STATUS  AGE
Wide:   NAME  [resource-specific columns...]  [extra columns...]  STATUS  AGE
```

Rules:
- **NAME** is always the first column. This is the slug-id or user-facing
  identifier — what users copy-paste into `get`/`update`/`delete` commands.
  Not `ID`, not `UID`, not `UUID` — always `NAME`.
- **STATUS** (if the resource has a state) is second-to-last. Normalized
  to `STATUS` everywhere — not `STATE`, `HEALTH`, `ENABLED`, or `ACTIVE`.
- **AGE** is always the last column. Relative time since creation (like
  kubectl's `AGE` column: `5d`, `2h`, `30m`). Replaces the current zoo of
  `CREATED`, `CREATED AT`, `CREATED_AT`, `TIME`, etc.
- Resource-specific columns go in the middle, ordered by importance.
- `wide` adds additional detail columns between the resource-specific
  columns and STATUS/AGE.

**Current state**: NAME is first in only ~25% of resources. Identifier
column names vary (ID, UID, UUID, INCIDENTID, USER_PK). Timestamp column
names and formats are inconsistent across all providers.

**Exemptions**: Signal query results (labels, series, metrics) don't
follow this convention — they output query-specific data, not CRUD
resources.

### Contract 2: JSON/YAML Envelope

All CRUD resources use the K8s metadata envelope for JSON and YAML output:

```yaml
apiVersion: <group>/<version>
kind: <Kind>
metadata:
  name: <slug-id>
  namespace: <namespace>
spec:
  <domain fields>
```

This is already the convention (provider-checklist.md) but not uniformly
applied. Key fixes: OnCall `get` subcommand, k6 resource types.

Non-CRUD commands (status, timeline, query results, operational views)
output their natural structure without the envelope.

### Contract 3: `status` Verb

`status` means "current health of a resource, merging API state with
live metrics." Standard contract:

```
gcx <provider> <resource> status [ID]    # ID optional for aggregate view
```

| Property | Standard |
|----------|----------|
| Default format | `table` |
| Required formats | `table`, `wide`, `json`, `yaml`, `graph` |
| Time range flags | None (always current state) |
| Filter flags | Provider-specific (e.g., `--label`, `--job`) |
| STATUS values | `OK`, `FAILING`, `NODATA` (normalized enum) |
| Graph format | Percentage bar chart or gauge |

Fixes needed:
- `kg status`: add table codec, normalize to standard contract
- `setup status`: add `-o` flag support
- Normalize STATUS enum: SLO's `BREACHING` → `FAILING` (or adopt a
  shared enum that maps both)

### Contract 4: `timeline` Verb

`timeline` means "time-series chart of a numeric signal over a range."
Standard contract:

```
gcx <provider> <resource> timeline [ID]
```

| Property | Standard |
|----------|----------|
| Default format | `graph` |
| Required formats | `graph`, `table`, `json`, `yaml` |
| Time range flags | `--from`, `--to`, `--since`, `--step` |
| Default range | `--since 7d` (normalized) |
| Graph format | Line chart, one series per sub-resource |

Fixes needed:
- Normalize `--since` default (synth uses `6h`, SLO uses `7d` via
  `--from now-7d`)
- Rename `assistant investigations timeline` → `activity` (it's an event
  log, not a time-series)

### Contract 5: Imperative vs Declarative Separation

Provider commands and the resources pipeline serve different purposes.
Keep them cleanly separated:

```
PROVIDER COMMANDS (imperative, explicit):
  create -f FILE           # Must not exist — fails on duplicate
  update <name> -f FILE    # Must exist — fails if missing
  delete <name>            # Explicit delete (prompt or --yes)
  get <name>               # Fetch one resource
  list                     # Fetch all resources

RESOURCES PIPELINE (declarative, GitOps):
  resources push -p DIR    # Create-or-update (upsert), idempotent
  resources pull -p DIR    # Export to disk
  resources delete -p DIR  # Delete what's in files
```

**Identifier terminology**: All positional resource identifiers are
called `<name>` in help text and documentation. Under the hood this may
be a slug-id (`grafana-instance-health-5594`), UUID, or UID — but the
user-facing term is always "name." This matches the `NAME` column in
table output (Contract 1) and the `metadata.name` field in K8s
manifests (Contract 2).

Rules:
- **No `push`/`pull` on providers.** SLO's `definitions push`/`pull` are
  historical accidents from before the resources pipeline existed — remove
  them. Users use `resources push slos -p ./slos/` instead.
- **No `apply` on CRUD resources.** The upsert semantic belongs in
  `resources push`. Provider `create` intentionally fails on duplicate as
  a safety guard.
- **`create -f` and `update ID -f` behave identically across all
  providers.** Same flag (`-f`), same file format (K8s envelope YAML/JSON),
  same error behavior. A user who knows `gcx slo definitions create -f`
  can predict `gcx fleet pipelines create -f`.

**Renames:**
- `recommendations apply` → `recommendations accept` (not a CRUD apply)
- `instrumentation apply` → TBD (refactor details separate; the
  instrumentation command tree is being redesigned in D2)

### Contract 6: `show` Verb — Singletons Only

`show` is reserved for singleton resources (no ID, only one exists).
All other usages are remapped to `get` or `list`:

| Current | New | Rationale |
|---------|-----|-----------|
| `appo11y overrides get` | `appo11y overrides show` | Singleton — no ID, one exists |
| `appo11y settings get` | `appo11y settings show` | Singleton |
| `instrumentation show <cluster>` | `instrumentation get <cluster>` | Has ID arg → `get` |
| `metrics recommendations show` | `metrics recommendations list` | Collection → `list` |
| `logs patterns show` | `logs patterns list` | Collection → `list` |
| `traces recommendations show` | `traces recommendations list` | Collection → `list` |
| `kg entities show [name]` | `kg entities get <name>` | By name → `get` |

Decision tree:
- Has an ID/name argument → `get`
- Returns a collection → `list`
- Singleton (no ID, exactly one resource) → `show`

### Contract 7: Action Verbs (Domain)

Action verbs are state transitions on existing resources. Standard
contract:

```
gcx <provider> <resource> <action> <name>
```

| Property | Standard |
|----------|----------|
| Args | Exactly 1: the resource name |
| Output | `cmdio.Success` to stderr (not stdout) |
| Formats | None needed (action confirmation only) |
| Reversible actions | Paired: `acknowledge`/`unacknowledge`, `silence`/`unsilence` |
| Irreversible actions | Require `--yes` or prompt on stderr |

Fixes needed:
- `incidents open` → `incidents browse` (opens browser, not a state change)
- `cancel` (investigations) should return success message, not YAML
- `silence --duration` should accept string duration (`1h`) not seconds
- `recommendations apply` → `recommendations accept` (not a CRUD apply)

### Contract 8: Query Commands (Signal)

Already standardized by PR #348. The canonical pattern:

```
gcx <signal> query EXPR -d <datasource-uid> --since <duration>
gcx <signal> labels -d <datasource-uid>
gcx <signal> metrics EXPR -d <datasource-uid> --since <duration>
```

| Property | Standard |
|----------|----------|
| Datasource flag | `-d` / `--datasource` (required) |
| Time range | `--from`, `--to`, `--since` (mutually exclusive groups) |
| Output formats | `table`, `wide`, `json`, `yaml`, `graph` |
| Limit | `--limit` (normalize default across signals) |

Signal-specific extensions (e.g., `traces get TRACE_ID`, `profiles
profile-types`) are documented as official pattern extensions.

---

## D10: Dashboard Provider

Dashboards are the most common Grafana resource but have no provider
commands — users must go through `resources get dashboards/my-uid` for
basic operations. Adding a dashboard provider with ergonomic commands,
domain-specific table output, and search.

### Command Tree

```
gcx dashboards list                          # Dashboard-specific table
gcx dashboards get <uid>                     # Full dashboard
gcx dashboards create -f dashboard.yaml      # Must not exist
gcx dashboards update <uid> -f dashboard.yaml # Must exist
gcx dashboards delete <uid>                  # With prompt / --yes
gcx dashboards search <query>                # Full-text search
gcx dashboards snapshot <uid>                # Already exists — PNG rendering
```

### Implementation

CRUD commands use the resources pipeline client under the hood (same K8s
API: `/apis/dashboard.grafana.app/...`). No separate API client needed —
the provider wraps the existing dynamic client with dashboard-specific
TypedCRUD and table codecs.

Search uses the K8s-native search endpoint:
`/apis/dashboard.grafana.app/v{preferred}/namespaces/{ns}/search`

Search is dashboard-only (the API doesn't support other resource types),
so it stays as a provider command, not `resources search`.

### Table Columns

Following Contract 1 (CRUD Table Columns):

```
Table:  NAME  TITLE  FOLDER  TAGS          STATUS  AGE
Wide:   NAME  TITLE  FOLDER  TAGS  PANELS  URL     STATUS  AGE
```

Search output:

```
Table:  NAME  TITLE  FOLDER  TAGS  AGE
```

### Grouping

`dashboards` stays in "Monitoring & Insights" — users think of dashboards
as monitoring, not infrastructure. The CRUD commands are convenience
shortcuts over the resources pipeline, not a signal that dashboards are
an infra primitive.

---

## Implementation Areas

Eight areas, ordered by dependency and user-facing impact. The first
five fix substance (how things work), the last three add features.

### Area 1: Login & Config Consolidation (D1)

**Goal**: `gcx login` replaces the manual 6-8 step config dance.
Reorganize auth/config — don't solve unsolved problems.

| Feature | Scope |
|---------|-------|
| `gcx login` wizard (interactive + flags) | Wire to existing OAuth + SA token auth |
| Multi-context support | Auto-derive context name from server |
| Two-step Cloud auth | Grafana auth + CAP token |
| Re-auth via `--context` | Update expired tokens |
| `auth` absorbed into `login` + `config` | Deprecation redirect |

**Out of scope**: Full e2e OAuth expansion (#310/#311), GCOM provider
(#125), token scope metadata (#122).

**Existing issues**: #363, #384 (partial)

### Area 2: Setup Framework (D2)

**Goal**: `gcx setup status` dashboard + provider interfaces.
Framework only — individual provider setups are Area 7.

| Feature | Scope |
|---------|-------|
| `gcx setup status` | Aggregated product status table |
| `Setupable` + `StatusDetectable` interfaces | Provider registration |
| `gcx setup init` | Guided multi-select onboarding |
| Instrumentation promoted to top-level | Move from `setup instrumentation` → `gcx instrumentation` |

**Existing issues**: #287, #319

### Area 3: Verb & CRUD Consistency (D6, D9 Contracts 1-2, 5-7)

**Goal**: Every CRUD command behaves the same. Verbs mean one thing.

| Feature | Scope |
|---------|-------|
| `show` → `get`/`list` renames | Per decision tree |
| Read-only adapter removal | alert, kg resources |
| Imperative/declarative split | Remove SLO `push`/`pull` |
| `recommendations apply` → `accept` | Verb rename |
| `incidents open` → `browse` | Verb rename |
| Verb metadata on adapter Registration | Static discovery |
| CRUD table columns | NAME first, STATUS, AGE |
| K8s envelope on all CRUD json/yaml | OnCall get, k6 fixes |
| `--yes` flag for destructive ops | Confirmation prompts |

**Existing issues**: #387, #283, #321, #241

### Area 4: Output & Error Consistency (D7, D9 Contracts 3-4)

**Goal**: Predictable, parseable output everywhere.

| Feature | Scope |
|---------|-------|
| `status` verb contract | table/wide/json/yaml/graph, normalized enum |
| `timeline` verb contract | graph default, --from/--to/--since/--step |
| `investigations timeline` → `activity` | Verb rename |
| Wide codec gaps (31 commands) | Prioritized by provider |
| Mutation summary table codec | Structured push/pull/delete output |
| Delete prompts to stderr | synth, slo fixes |
| Provider error handling | k8s apierrors |
| Exit code documentation | Codify and document |
| Document undocumented UX behaviors | Behavioral contracts |

**Existing issues**: #264, #256, #158, #388

### Area 5: Signal Provider Fixes (D8)

**Goal**: Fix bugs and document patterns in metrics/logs/traces/profiles.

| Feature | Scope |
|---------|-------|
| Fix `--step` on traces/profiles query | Remove flag or wire to request |
| Fix `-o wide` crash on profiles query | Add missing codec case |
| Normalize `--limit` defaults | Shared default or documented rationale |
| Document adaptive sub-tree divergence | `docs/design/` |
| Document signal-specific extensions | traces get, profile-types |

**Existing issues**: #425 (partial)

### Area 6: CLI Surface Reorganization (D3, D4, D5)

**Goal**: Make `gcx --help` navigable. No behavior changes.

| Feature | Scope |
|---------|-------|
| 7 Cobra command groups | Group-aware help rendering |
| First-run detection | Welcome message on empty config |
| Post-command suggestions | Next steps to stderr |

**Existing issues**: #240, #27

### Area 7: Provider Setup Commands

**Goal**: Wire `Setupable` to providers with existing infrastructure.
Depends on Area 2 (framework).

| Provider | Setup command | What it does |
|----------|-------------|-------------|
| `synth` | `gcx synth setup --url <target>` | SM init + credential auto-config |
| `frontend` | `gcx frontend setup --name <app>` | Create Faro app + output SDK snippet |
| `instrumentation` | `gcx instrumentation setup <cluster>` | Already exists, promoted from `setup instrumentation` |
| `appo11y` | `gcx appo11y setup` | Enable App O11y + configure defaults |

**Out of scope**: SLO setup (needs query builder wizard), OnCall setup
(needs dependency-chain wizard), alert setup (needs Area 8 write support).

### Area 8: New Provider Features (D10)

**Goal**: Dashboard CRUD + alerting write support.

| Feature | Provider | Scope |
|---------|----------|-------|
| Dashboard CRUD (list/get/create/update/delete) | dashboards | New provider wrapping resources pipeline |
| Dashboard search | dashboards | K8s-native search endpoint |
| Alert rules write support | alert | Add create/update/delete |
| Contact points + notification policies | alert | New resource types |

**Existing issues**: #469, #320

---

## Open Questions

1. **`connections` provider API**: What's the programmatic API for the
   Grafana Integrations catalog? Need to verify REST API exists.
2. **`csp` and `db` provider scope**: Start with one cloud provider / DB
   type or all from day one?
3. **Group name alignment**: Align group names with Grafana's existing
   product terminology as v1. Internal polling can refine later.
