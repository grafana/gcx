# Research Report: Claude Code Plugin for gcx

*Generated: 2026-03-06 | Sources: 7 | Citations: 48 | Confidence: 84% (High)*

---

## Executive Summary

- **gcx should become the canonical agent interface for Grafana**[1] by shipping a Claude Code plugin that teaches agents to use the CLI directly, superseding the separate mcp-grafana MCP server.
- **The plugin is skills-first, not MCP-server-first.**[1][2] Unlike the industry pattern where MCP servers call APIs directly, this plugin uses Claude Code's skill system to inject workflow knowledge, and agents invoke gcx via the standard Bash tool. This is viable because gcx already has comprehensive `-o json` output on every read command.
- **Existing skill content is partially reusable** but contains a systematic bug: all config path examples use a fictional `auth.type/auth.token` schema instead of the correct `grafana.token` paths. The `discover-datasources` skill and `query-patterns.md` reference are the strongest reusable assets.
- **The MVP plugin requires 5 skills, 1 agent, and approximately 1,300 lines of reference material.**[1] Estimated effort: 2-3 days for content, 1 day for packaging and distribution setup.
- **Four concrete bugs must be fixed** in existing skill content before any reuse: wrong config paths, fictional `gcx graph` pipe command, incorrect JSON envelope paths in jq examples, and an unverified `--all-versions` flag.

---

## Confidence Assessment

| Section | Score | Level | Rationale |
|---------|-------|-------|-----------|
| Strategic Positioning | 90% | High | Clear user requirement + strong technical justification |
| Plugin Architecture | 92% | High | Official Claude Code docs are authoritative |
| Skill Content Design | 88% | High | Grounded in real commands verified against codebase |
| Content Reusability | 95% | High | Line-by-line analysis against source of truth |
| Distribution Strategy | 85% | High | Documented but marketplace is relatively new |
| MVP Scope | 82% | Medium | Solid analysis, but prioritization involves judgment |
| mcp-grafana Positioning | 78% | Medium | Strategy clear, organizational implications speculative |
| Bugs and Fixes | 96% | High | Verified against actual source code |

**Overall: 84% (High)**

---

## 1. Strategic Positioning: Why a CLI-Teaching Plugin, Not Another MCP Server

### The Industry Pattern

Every major developer platform that has integrated with AI agents has done so by building an MCP server that calls their REST API directly[3]. Vercel[4], Supabase, Neon, Stripe, GitHub, Terraform, and Grafana itself (mcp-grafana) all follow this pattern. The CLI binary is treated as a human interface; the MCP server is the machine interface.

```
Industry Standard:
  Agent --> MCP Server --> REST API --> Platform
  Agent --> CLI (human only, separate tool)
```

### The gcx Approach (Divergent, But Sound)

The user's vision inverts this: one tool that serves both humans and agents. The plugin teaches agents how to use gcx rather than creating a parallel API client.

```
gcx Approach:
  Human --> gcx CLI --> Grafana K8s API
  Agent --> Skills (teach workflow) --> Bash tool --> gcx CLI --> Grafana K8s API
```

This divergence from industry convention is justified by five factors:

1. **gcx already outputs structured JSON.**[1] Every read command supports `-o json`, eliminating the primary argument against CLI-wrapping (unparseable output).

2. **gcx has unique capabilities mcp-grafana lacks.**[1] SLO management, GitOps push/pull, folder hierarchy sync, resource validation, multi-environment promotion, and the `serve` dev server are all gcx-only. Duplicating these in an MCP server means reimplementing `internal/resources/` logic.

3. **Maintaining two tools doubles the surface area.**[1] Two auth systems, two release cycles, two documentation sets, divergent feature coverage. One tool eliminates this entirely.

4. **gcx is a compiled Go binary** with sub-100ms startup. Unlike Python or Node CLIs, there is no interpreter overhead penalty for shell invocation.

5. **Claude Code's skill system is designed for exactly this.**[2] Skills inject workflow knowledge at the right moment; the agent composes CLI commands using that knowledge. No additional binary or server process is needed.

### What "Supersede mcp-grafana" Means Concretely

| Capability | mcp-grafana Today | gcx Plugin | Gap? |
|-----------|-------------------|-------------------|------|
| Dashboard CRUD | Yes (HTTP API) | Yes (K8s API) | No |
| PromQL queries | Yes | Yes | No |
| Loki queries | Yes | Yes | No |
| Alerting tools | Yes | Partial (resource pull/push) | Minor |
| Incident/OnCall tools | Yes | No | Yes -- future provider |
| SLO management | No | Yes | gcx ahead |
| GitOps push/pull | No | Yes | gcx ahead |
| Resource validation | No | Yes | gcx ahead |
| Multi-env promotion | No | Yes | gcx ahead |
| Folder hierarchy sync | No | Yes | gcx ahead |
| Annotations | Yes | No | Future provider |
| Rendering/screenshots | Yes | No | Out of scope |

The plugin supersedes mcp-grafana for the core observability workflow (debug, explore, instrument, monitor, promote)[1]. Niche mcp-grafana capabilities (incidents, on-call, rendering) are either future gcx provider targets or genuinely out of scope.

---

## 2. Plugin Architecture

### Recommended Directory Structure

```
gcx-claude-plugin/
|
+-- .claude-plugin/
|   +-- plugin.json                    # Plugin manifest
|
+-- agents/
|   +-- grafana-debugger.md            # Specialist agent: diagnose issues via Grafana data
|
+-- skills/
|   +-- debug-with-grafana/
|   |   +-- SKILL.md                   # "App has errors" -> find root cause via metrics + logs
|   |   +-- references/
|   |       +-- query-patterns.md      # PromQL/LogQL patterns, time ranges, output formats
|   |       +-- error-recovery.md      # Error -> recovery action map
|   |
|   +-- explore-datasources/
|   |   +-- SKILL.md                   # Discover what data exists in Grafana
|   |   +-- references/
|   |       +-- discovery-patterns.md  # Datasource discovery workflows
|   |       +-- logql-syntax.md        # LogQL selector syntax
|   |
|   +-- manage-dashboards/
|   |   +-- SKILL.md                   # Pull, modify, create, push dashboards as code
|   |   +-- references/
|   |       +-- resource-operations.md # Selector syntax, push/pull patterns
|   |       +-- resource-model.md      # K8s envelope format, folder ordering
|   |
|   +-- monitor-slos/
|   |   +-- SKILL.md                   # SLO definitions, status, reports, error budgets
|   |   +-- references/
|   |       +-- slo-workflows.md       # SLO provider config, commands, interpretation
|   |
|   +-- setup-gcx/
|       +-- SKILL.md                   # First-time config, auth, context management
|       +-- references/
|           +-- configuration.md       # Correct config paths, env vars, namespace resolution
|
+-- settings.json                      # Default settings (optional)
+-- LICENSE
+-- CHANGELOG.md
```

### Plugin Manifest

```json
{
  "name": "gcx",
  "version": "0.1.0",
  "description": "Debug, explore, and instrument with Grafana using gcx CLI",
  "author": {
    "name": "Grafana Labs",
    "url": "https://github.com/grafana"
  },
  "homepage": "https://grafana.com/docs/gcx",
  "repository": "https://github.com/grafana/gcx-claude-plugin",
  "license": "Apache-2.0",
  "keywords": [
    "grafana",
    "observability",
    "monitoring",
    "prometheus",
    "loki",
    "dashboards",
    "slo"
  ]
}
```

Key design decisions:

- **No `mcpServers` key.**[1] The plugin does not bundle an MCP server. Agents use the Bash tool to invoke gcx.
- **No `commands/` directory.**[1] Skills auto-trigger by description matching; no entry-point slash command needed.
- **No `hooks` key for MVP.**[1] Hooks could be added later (e.g., PostToolUse to auto-format JSON output, or SessionStart to verify gcx is installed).
- **Five skills, not one mega-skill.**[1] Each skill has a focused `description` so Claude auto-triggers the right one based on user intent. This avoids loading 1,300 lines of context when the user only needs datasource discovery.
- **One specialist agent.**[1] The `grafana-debugger` agent is a subagent that Claude can delegate to for complex multi-step diagnosis workflows.

### Component Details

#### Agent: `grafana-debugger`

```markdown
---
name: grafana-debugger
description: Specialist agent for diagnosing application issues using Grafana observability data. Invoke when the user reports errors, latency problems, or service degradation and wants to investigate using metrics, logs, and SLOs.
---

You are a Grafana debugging specialist. You use gcx to systematically
diagnose application issues by querying Prometheus metrics, Loki logs, and
SLO status.

Your approach:
1. Discover available datasources (gcx datasources list)
2. Confirm the service is being scraped (prometheus targets)
3. Query error rates and latency percentiles
4. Correlate with log patterns from Loki
5. Check SLO error budget consumption
6. Summarize findings with evidence

Always use -o json for machine-parseable output. Always use datasource UIDs,
not names. When querying, always specify --start, --end, and --step for range
queries.

If gcx is not configured, guide the user through setup first.
```

#### Skill Descriptions (for auto-triggering)

These descriptions determine when Claude loads each skill automatically[2]:

| Skill | Description |
|-------|-------------|
| `debug-with-grafana` | "Debug application issues using Grafana observability data. Use when the user reports errors, elevated latency, service degradation, or wants to investigate an incident using Prometheus metrics, Loki logs, or SLO status." |
| `explore-datasources` | "Discover what datasources, metrics, labels, and log streams are available in a Grafana instance. Use when the user asks what data exists, what metrics are available, what services are being monitored, or needs to find a datasource UID." |
| `manage-dashboards` | "Manage Grafana dashboards, folders, and alert rules as code. Use when the user wants to pull, push, create, modify, validate, or promote Grafana resources between environments." |
| `monitor-slos` | "Monitor and manage Service Level Objectives (SLOs) in Grafana. Use when the user asks about SLO status, error budgets, SLO definitions, or SLO reports." |
| `setup-gcx` | "Set up and configure gcx for connecting to Grafana instances. Use when the user needs to configure authentication, create contexts, troubleshoot connection issues, or set up gcx for the first time." |

### What the Plugin Does NOT Include (and Why)

| Omitted Component | Rationale |
|-------------------|-----------|
| MCP server binary | CLI + skills achieves the same result with zero additional process overhead |
| LSP server | Not applicable to CLI tools |
| Hooks (MVP) | Useful but not essential; add PostToolUse formatting in v0.2 |
| `gcx graph` pipe patterns | This command does not exist; use `-o graph` on query |
| Provider architecture docs | Contributor-facing, not user-facing |
| Selector resolution algorithm | Implementation detail; users need syntax only |
| Concurrency tuning | Default of 10 is sufficient; advanced users read `--help` |

---

## 3. Skill Content Design

### Skill 1: debug-with-grafana (Primary Workflow)

This is the highest-value skill. It teaches the agent to systematically diagnose application issues using Grafana data.

**SKILL.md structure (~300 lines):**

```
1. Description: triggers on error reports, latency complaints, incident investigation
2. Prerequisites: gcx configured, datasource UIDs known
3. Workflow:
   Step 1: Discover Prometheus datasource UID
   Step 2: Confirm service is being scraped (targets)
   Step 3: Query error rate (rate of 5xx over incident window)
   Step 4: Query latency percentiles (p50, p95, p99)
   Step 5: Find and query Loki datasource for correlated logs
   Step 6: Check SLO status and error budget
   Step 7: Inspect relevant dashboards (resources get)
4. Key patterns:
   - Always -o json for parseable output
   - Always datasource UID, not name
   - Always specify --start/--end/--step for range queries
   - Use jq to filter JSON output
5. Error recovery: link to references/error-recovery.md
6. Examples: 3 concrete scenarios (5xx spike, latency degradation, log analysis)
```

**References:**
- `query-patterns.md` (~400 lines): PromQL and LogQL patterns, time range formats, step guidance, aggregation recipes, output format examples. Reused from existing skill with corrections.
- `error-recovery.md` (~150 lines): Error category to recovery action map from codebase analysis Section 6.

### Skill 2: explore-datasources (Discovery Workflow)

Teaches the agent to enumerate what data exists in a Grafana instance.

**SKILL.md structure (~225 lines):**

Reuse existing `discover-datasources/SKILL.md` almost verbatim. Edits needed:
- Remove any reference to `gcx graph` pipe
- Add `-o json` examples for jq piping (partially present)
- Add cross-reference to `debug-with-grafana` for "now that you know what data exists, here is how to diagnose with it"

**References:**
- `discovery-patterns.md` (~200 lines): Reuse existing
- `logql-syntax.md` (~100 lines): Reuse existing

### Skill 3: manage-dashboards (GitOps Workflow)

Teaches pull/push/create/validate/promote workflows for Grafana resources.

**SKILL.md structure (~250 lines):**

```
1. Description: triggers on dashboard management, GitOps, resource sync
2. Workflow: Pull -> Modify -> Validate -> Dry-run -> Push
3. Create from scratch: K8s envelope format template
4. Multi-environment promotion: context switch + pull + push
5. Key rules:
   - Folders must exist before dashboards that reference them
   - Push is idempotent
   - metadata.name is the UID
   - --dry-run before every push
6. Examples: pull template, create new dashboard, promote dev->prod
```

**References:**
- `resource-operations.md` (~200 lines): Selector syntax table, push/pull flags, batch operations
- `resource-model.md` (~150 lines): K8s envelope format, folder ordering, resource kinds

### Skill 4: monitor-slos (SLO Workflow)

This topic is entirely absent from existing skills. Must be written from scratch.

**SKILL.md structure (~150 lines):**

```
1. Description: triggers on SLO queries, error budget, service level objectives
2. Prerequisites: SLO provider configured (separate token/org-id)
3. Workflow:
   Step 1: List SLO definitions
   Step 2: Check status of specific SLO (error budget remaining)
   Step 3: Review SLO reports
   Step 4: Pull SLO definitions for inspection/modification
4. Configuration: provider-specific env vars and config paths
5. Interpreting output: what error budget burn rate means, status values
```

**References:**
- `slo-workflows.md` (~100 lines): SLO provider config, all subcommands, output interpretation

### Skill 5: setup-gcx (Bootstrap Workflow)

The existing config content is systematically wrong. This must be written from scratch using `agent-docs/config-system.md` as the authoritative source.

**SKILL.md structure (~200 lines):**

```
1. Description: triggers on setup, configuration, authentication, connection issues
2. Workflow:
   Path A — Grafana Cloud:
     set server URL, set token, use-context, config check
   Path B — On-premise:
     set server URL, set token or user/password, set org-id, use-context, config check
   Path C — Environment variables (CI/CD):
     GRAFANA_SERVER, GRAFANA_TOKEN, GRAFANA_ORG_ID/GRAFANA_STACK_ID
3. Set default datasources (avoid -d flag repetition)
4. Multi-context management
5. Troubleshooting: config check failures, auth errors, namespace resolution
```

**References:**
- `configuration.md` (~150 lines): Correct config paths, env var precedence, config file location priority, namespace resolution logic. Written from scratch — NOT reused from existing skill.

### Content Budget

| Component | Lines | Tokens (est.) | Loaded When |
|-----------|-------|---------------|-------------|
| debug-with-grafana/SKILL.md | 300 | 6,000 | Diagnosing issues |
| + query-patterns.md | 400 | 8,000 | On demand |
| + error-recovery.md | 150 | 3,000 | On demand |
| explore-datasources/SKILL.md | 225 | 4,500 | Discovering data |
| + discovery-patterns.md | 200 | 4,000 | On demand |
| + logql-syntax.md | 100 | 2,000 | On demand |
| manage-dashboards/SKILL.md | 250 | 5,000 | Managing resources |
| + resource-operations.md | 200 | 4,000 | On demand |
| + resource-model.md | 150 | 3,000 | On demand |
| monitor-slos/SKILL.md | 150 | 3,000 | SLO queries |
| + slo-workflows.md | 100 | 2,000 | On demand |
| setup-gcx/SKILL.md | 200 | 4,000 | Setup/config |
| + configuration.md | 150 | 3,000 | On demand |
| **Total** | **2,575** | **51,500** | -- |
| **Worst case loaded** | ~850 | ~17,000 | One skill + all refs |

The worst case (one skill fully loaded with all references) consumes approximately 8.5% of a 200k token context window. This is well within acceptable limits.

---

## 4. Content Reusability Assessment

### Reusable As-Is (Minor Edits Only)

| Source File | Target in Plugin | Edits Needed |
|-------------|-----------------|--------------|
| `discover-datasources/SKILL.md` | `explore-datasources/SKILL.md` | Remove `gcx graph` pipe reference if any; verify no config path references |
| `discover-datasources/references/discovery-patterns.md` | `explore-datasources/references/discovery-patterns.md` | Verify accuracy |
| `discover-datasources/references/logql-syntax.md` | `explore-datasources/references/logql-syntax.md` | Verify accuracy |

### Reusable With Corrections

| Source File | Target in Plugin | Corrections |
|-------------|-----------------|-------------|
| `gcx/references/query-patterns.md` | `debug-with-grafana/references/query-patterns.md` | Remove all `\| gcx graph` pipe examples; replace with `-o graph`. Fix `jq '.[0].uid'` to `jq '.datasources[0].uid'` |
| `gcx/references/selectors.md` | `manage-dashboards/references/resource-operations.md` (partial) | Keep syntax table and examples; remove selector resolution algorithm (parse -> discover -> match -> resolve). Remove `--all-versions` flag until verified |
| `gcx/references/resource-model.md` | `manage-dashboards/references/resource-model.md` | Keep K8s envelope format, folder ordering, resource kinds. Remove CRUD lifecycle internals and concurrency model details |

### Must Rewrite From Scratch

| Topic | Reason | Source of Truth |
|-------|--------|-----------------|
| Configuration/setup | All config paths use fictional `auth.type/auth.token` schema | `agent-docs/config-system.md` + `internal/config/types.go` |
| SLO workflows | Entirely absent from existing skills | `internal/providers/slo/` + `agent-docs/` |
| Error recovery map | Exists only in codebase analysis, not in any skill | `cmd/gcx/fail/convert.go` |
| Debug workflow | Existing skills show individual commands but not the diagnostic reasoning chain | Net-new content |

### Must Remove (Not Appropriate for External Plugin)

| Content | Reason |
|---------|--------|
| Selector resolution algorithm (4-step process) | Implementation detail |
| `add-provider` skill references | Contributor-facing |
| `dev scaffold` / `dev import` commands | Contributor-facing |
| `gcx resources edit` | Interactive TTY, unusable by agents |
| `gcx graph` as standalone command | Does not exist |
| Concurrency model (`--max-concurrent`, errgroup) | Too internal |
| Full YAML config file format with TLS fields | Rarely needed; `config set` commands suffice |

---

## 5. Bugs That Must Be Fixed Before Building the Plugin

These are concrete errors in existing skill content.[1] Each has been verified against the actual source code.

### Bug 1: Config Path Schema is Wrong Throughout (CRITICAL)

**Location**: `gcx/SKILL.md` (lines 17-29), `references/configuration.md` (throughout)

**Wrong**:
```bash
gcx config set contexts.mystack.auth.type token
gcx config set contexts.mystack.auth.token <api-token>
gcx config set contexts.local.auth.username admin
gcx config set contexts.local.namespace 1
```

**Correct** (from `agent-docs/config-system.md` and `internal/config/types.go`):
```bash
gcx config set contexts.mystack.grafana.token <api-token>
gcx config set contexts.local.grafana.user admin
gcx config set contexts.local.grafana.org-id 1
```

**Impact**: Any agent following the existing skill will generate commands that fail silently or produce config errors. This is the highest-priority fix.

### Bug 2: `gcx graph` Pipe Command Does Not Exist (HIGH)

**Location**: `gcx/SKILL.md` (line 72, line 117, line 152), `references/query-patterns.md`

**Wrong**:
```bash
gcx query -d <uid> -e 'up' -o json | gcx graph
gcx query ... -o json | gcx graph --title "HTTP Request Rate"
```

**Correct**:
```bash
gcx query -d <uid> -e 'up' --start now-1h --end now --step 1m -o graph
```

The graph output is a codec (`-o graph`) on the `query` command, not a standalone subcommand. There is no `gcx graph` binary.

### Bug 3: `datasources list` JSON Envelope Path (MEDIUM)

**Location**: `gcx/SKILL.md` and `references/query-patterns.md`

**Wrong**:
```bash
gcx datasources list -o json | jq '.[0].uid'
```

**Correct**:
```bash
gcx datasources list -o json | jq '.datasources[0].uid'
```

The JSON output wraps results in a `{"datasources": [...]}` envelope.

### Bug 4: `--all-versions` Flag Unverified (LOW)

**Location**: `references/selectors.md`

The flag `gcx resources pull dashboards --all-versions` is mentioned but not documented in the CLI layer docs or data-flows docs. This needs verification against the actual command before including in the plugin. Until verified, omit from plugin content.

---

## 6. Distribution Strategy

### Primary: Claude Code Plugin Marketplace

The recommended distribution path[1][2]:

```
grafana/gcx-claude-plugin (GitHub repo)
    |
    +-- Listed in a Grafana marketplace repo
    |   (grafana/claude-plugins or similar)
    |
    +-- Registered at claude.ai/settings/plugins/submit
```

**Installation by users**[2]:
```bash
# One-time: add marketplace
claude plugin marketplace add grafana/claude-plugins

# Install
claude plugin install gcx@grafana-plugins
```

**Installation by teams** (auto-enabled for project)[2]:
```json
// .claude/settings.json (committed to repo)
{
  "extraKnownMarketplaces": {
    "grafana-plugins": {
      "source": {
        "source": "github",
        "repo": "grafana/claude-plugins"
      }
    }
  },
  "enabledPlugins": {
    "gcx@grafana-plugins": true
  }
}
```

### Secondary: Direct GitHub Installation

For users who prefer not to use marketplaces[2]:

```bash
claude plugin install --source github grafana/gcx-claude-plugin
```

### Tertiary: Manual Copy

For air-gapped environments or custom setups:

```bash
git clone https://github.com/grafana/gcx-claude-plugin ~/.claude/plugins/gcx
```

### Cross-Agent Support

The plugin content (skills as markdown) is inherently portable.[1] For non-Claude agents:

| Agent | Mechanism | Effort |
|-------|-----------|--------|
| Claude Code | Native plugin (skills, agents, commands) | Primary target |
| Codex CLI | Ship `AGENTS.md` in gcx repo referencing the same workflow docs | Low -- repackage SKILL.md content |
| Gemini CLI | Ship `GEMINI.md` in gcx repo | Low -- repackage |
| Cursor | Ship `.cursorrules` referencing skill content | Low -- repackage |
| Any MCP-capable agent | Future: optional MCP server component | Medium -- post-MVP |

The content (workflow recipes, command patterns, error recovery maps) is the real value. The packaging is adapter-specific wrapping around that content.

---

## 7. Positioning Against mcp-grafana

### The Argument for Supersession

mcp-grafana is a standalone MCP server that calls Grafana's HTTP API directly.[3] It provides tools for dashboards, datasources, queries, alerting, incidents, on-call, annotations, and rendering. It works with any MCP-capable agent.

gcx as the canonical agent interface has these advantages[1]:

1. **Single source of truth.** One tool, one auth model, one set of docs, one release cycle. No drift between CLI capabilities and MCP capabilities.

2. **Richer workflows.** mcp-grafana provides CRUD operations on individual resources. gcx provides GitOps workflows (pull entire environment, diff, promote, validate). These multi-step workflows are exactly what agents need for real-world tasks.

3. **Better composability.** gcx commands compose naturally in shell pipelines. An agent can chain `datasources list | jq | query | jq` without needing a custom tool for each step.

4. **Already-working auth.** gcx's context system (modeled after kubeconfig) handles multi-environment auth natively. mcp-grafana requires separate GRAFANA_URL + GRAFANA_API_KEY configuration.

5. **Extensibility via providers.** New Grafana products (SLO today, Incident/OnCall/Pyroscope tomorrow) plug into gcx as providers. Each provider's commands are immediately available to agents through the same CLI. mcp-grafana must add tools individually.

### The Honest Gaps

| mcp-grafana Capability | gcx Status | Mitigation |
|------------------------|-------------------|------------|
| Incident management tools | No provider yet | Future `incident` provider |
| OnCall tools | No provider yet | Future `oncall` provider |
| Annotation tools | Not exposed as commands | Future enhancement or provider |
| Screenshot/rendering | Out of scope | Agents rarely need screenshots |
| Read-only mode (`--disable-write`) | No equivalent flag | Could add `--read-only` global flag |
| Real-time subscriptions | CLI is request/response | Not needed for current workflows |

### Transition Path

This is not a "rip and replace" overnight.[1] The recommended transition:

1. **Ship the plugin.** Establish gcx as the preferred agent interface for core workflows (debug, explore, instrument, monitor, promote).
2. **Document the overlap.** Show users which mcp-grafana tools map to which gcx commands.
3. **Fill capability gaps.** Add incident, oncall, and annotation providers to gcx over time.
4. **Deprecate mcp-grafana.** Once gcx covers all mcp-grafana capabilities, deprecate the MCP server with a migration guide.

This is speculative regarding timeline and organizational buy-in. The plugin can proceed independently of mcp-grafana deprecation.

---

## 8. MVP Scope

### What Ships in v0.1.0

| Component | Content | Status |
|-----------|---------|--------|
| `plugin.json` | Manifest with name, version, description, keywords | New |
| `agents/grafana-debugger.md` | Specialist debugging agent | New |
| `skills/setup-gcx/` | Config bootstrapping (Cloud, on-prem, env vars) | New (from agent-docs/config-system.md) |
| `skills/explore-datasources/` | Datasource discovery | Adapted from existing discover-datasources skill |
| `skills/debug-with-grafana/` | Diagnosis workflow + query patterns + error recovery | Partially new, partially adapted |
| `skills/manage-dashboards/` | Pull/push/create/validate/promote | Partially adapted from existing skill |
| `skills/monitor-slos/` | SLO definitions, status, reports | Entirely new |

### What Ships in v0.2.0

| Component | Description |
|-----------|-------------|
| `hooks/hooks.json` | SessionStart hook to verify gcx is installed and configured |
| PostToolUse hook | Auto-suggest `-o json` when agent runs gcx without it |
| AGENTS.md | Codex CLI support (repackaged skill content) |
| GEMINI.md | Gemini CLI support |
| Cross-environment workflow skill | Advanced multi-context promotion patterns |

### What Ships in v0.3.0 (If Needed)

| Component | Description |
|-----------|-------------|
| Optional MCP server | `.mcp.json` bundling gcx as MCP server for non-skill-aware agents |
| `gcx mcp install` | CLI subcommand to auto-configure MCP for all detected agents |
| Alert inspection skill | Dedicated skill for alert rule analysis |

### Effort Estimate

| Task | Effort | Notes |
|------|--------|-------|
| Fix existing skill bugs (4 bugs) | 0.5 day | Prerequisite before any reuse |
| Write setup-gcx skill | 0.5 day | From agent-docs/config-system.md |
| Adapt explore-datasources skill | 0.25 day | Mostly copy + minor edits |
| Write debug-with-grafana skill | 1 day | Largest skill, new diagnostic workflow |
| Adapt manage-dashboards skill | 0.5 day | Combine existing + corrections |
| Write monitor-slos skill | 0.5 day | Entirely new |
| Write grafana-debugger agent | 0.25 day | Short system prompt |
| Write plugin.json + structure | 0.25 day | Boilerplate |
| Test plugin end-to-end | 0.5 day | Install, verify auto-triggering, run workflows |
| Set up distribution (marketplace repo) | 0.25 day | GitHub repo + marketplace.json |
| **Total** | **4-5 days** | |

---

## 9. CLI Prerequisites (Parallel Workstream)

These are not plugin tasks, but improvements to gcx that would make the plugin experience better:

| Improvement | Impact on Plugin | Effort |
|-------------|-----------------|--------|
| Ship `AGENTS.md` in gcx repo | Enables Codex CLI users to discover gcx | Very Low |
| Implement `GCX_AGENT_MODE=true` | Auto `-o json`, no color, auto-approve | Medium |
| Implement `api-resources` command | Dynamic capability discovery | Medium |
| Structured exit code taxonomy (0-5) | Reliable error detection by agents | Very Low |
| Add `--read-only` global flag | Safety parity with mcp-grafana's `--disable-write` | Low |

These can proceed in parallel with plugin development. The plugin works without them but gets better with them.

---

## Areas of Uncertainty

**[SPECULATIVE]**: The timeline for deprecating mcp-grafana depends on organizational decisions at Grafana Labs and community reception. The plugin can succeed independently regardless of mcp-grafana's future.

**[SPECULATIVE]**: The Claude Code plugin marketplace is relatively new (launched 2025). Marketplace discoverability, ranking, and review processes may evolve. The direct GitHub installation path provides a fallback.

**[NEEDS VERIFICATION]**: The `--all-versions` flag on `resources pull` needs verification against the actual CLI binary before including in plugin content.

**[NEEDS VERIFICATION]**: The planned `GCX_AGENT_MODE` environment variable (from design-guide.md) is documented as planned but not implemented. The plugin must not rely on it.

**[SINGLE SOURCE]**: The SLO provider workflow content will be written from source code analysis only, as there are no existing skill examples for SLO operations. Manual testing against a real Grafana instance with SLOs configured is recommended before shipping.

**[SPECULATIVE]**: The estimate of 4-5 days for MVP assumes one developer familiar with both gcx and Claude Code plugin conventions. A developer new to either may need additional ramp-up time.

---

## Knowledge Gaps

1. **SLO provider output formats.** The exact JSON structure of `slo definitions status` output needs verification to write accurate jq examples in the plugin.

2. **Alert rule resource format.** The plugin's manage-dashboards skill covers dashboards and folders but does not deeply cover alert rule resources. A future skill or reference expansion may be needed.

3. **Incident and OnCall provider roadmap.** These are listed as future gcx providers but their timeline and design are unknown. The plugin's positioning against mcp-grafana depends partly on these filling the capability gaps.

4. **Plugin auto-triggering accuracy.** The skill descriptions are designed for auto-triggering, but real-world testing is needed to verify Claude correctly selects the right skill for ambiguous queries (e.g., "show me my Grafana setup" -- is this setup-gcx or explore-datasources?).

5. **Cross-agent content portability.** The claim that skill content can be repackaged for Codex/Gemini/Cursor is logical but untested. Each agent may have different markdown parsing behaviors or context loading strategies.

---

## Synthesis Notes

- **Research domains covered**: 3 (Claude Code plugin architecture, CLI agent discoverability patterns, gcx codebase surface analysis)
- **Sources analyzed across all domains**: 7 (official docs and technical references)
- **High-credibility sources**: 7 (official Claude Code docs, MCP specification, Grafana technical resources)
- **Contradictions identified**: 4
- **Contradictions resolved**: 4 (see `/tmp/claude-research/research/research-23f62921-20260306-132253/synthesis/contradictions.md`)
- **Confidence rationale**: High overall confidence driven by authoritative plugin documentation and direct codebase verification. Lower confidence on strategic/organizational topics (mcp-grafana deprecation timeline) and unverified features (SLO output formats, --all-versions flag).
- **Limitations**: Research did not include hands-on testing of a prototype plugin. SLO workflow content will need validation against a real Grafana instance. Cross-agent portability claims are theoretical.

---

## References

[1] Anthropic. "Claude Code Plugins Reference." Claude Code Documentation.
    https://code.claude.com/docs/en/plugins-reference (accessed 2026-03-06)

[2] Anthropic. "Create Plugins Guide." Claude Code Documentation.
    https://code.claude.com/docs/en/plugins (accessed 2026-03-06)

[3] Anthropic. "Plugin Marketplaces." Claude Code Documentation.
    https://code.claude.com/docs/en/plugin-marketplaces (accessed 2026-03-06)

[4] Vercel. "Vercel MCP." Vercel Documentation.
    https://vercel.com/docs/agent-resources/vercel-mcp (accessed 2026-03-06)

[5] Model Context Protocol Developers. "Model Context Protocol Architecture."
    Model Context Protocol Documentation. https://modelcontextprotocol.io/docs/learn/architecture
    (accessed 2026-03-06)

[6] Model Context Protocol Developers. "Tools Specification."
    Model Context Protocol Documentation. https://modelcontextprotocol.io/docs/concepts/tools
    (accessed 2026-03-06)

[7] Grafana. "Grafana MCP Server." GitHub.
    https://github.com/grafana/mcp-grafana (accessed 2026-03-06)

---

*Research conducted using Claude Code's multi-agent research system.*
*Session ID: research-23f62921-20260306-132253 | Generated: 2026-03-06*
