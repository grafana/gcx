# Synthesis Report: Claude Code Skills for gcx Synthetic Monitoring

*Generated: 2026-03-07 | Research Domains: 10 | Sources: 8 | Citations: 24 | Overall Confidence: 89% (High)*

---

## 1. Executive Summary

**Recommendation: Create 3 focused skills for synthetic monitoring.**

| # | Skill Name | Primary User Intent | Key Commands |
|---|-----------|-------------------|--------------|
| 1 | `synth-check-status` | "How are my checks doing?" | `status`, `timeline`, `checks list` |
| 2 | `synth-investigate-check` | "Why is this check failing?" | `status`, `timeline`, `checks get`, `probes list`, `gcx query` |
| 3 | `synth-manage-checks` | "Create/update/organize my checks" | `push`, `pull`, `delete`, `checks list`, `probes list` |

This decomposition follows the established project convention of one skill per distinct user intent[4]. Each skill has clear, non-overlapping triggers and stays under 400 lines. Probe management is embedded within these skills (since gcx only supports `probes list`, which is a supporting step, not a standalone workflow).

---

## 2. Skill Decomposition Rationale

### Why 3 Skills (Not 1, Not 5+)

**Against 1 monolithic skill:**
- The skill architecture research is unambiguous: "focused single-domain skills beat monolithic[1]." The three use cases have genuinely different triggers ("check my uptime" vs "why is check failing" vs "create a check for my API"), different workflow shapes (glance vs investigation vs configuration), and different output modes (table/graph vs analysis text vs YAML files).
- A combined skill would exceed 500 lines easily and require a complex description with multiple OR clauses -- both are documented split signals[2].

**Against 5+ micro-skills:**
- Splitting `status` from `timeline` would be artificial -- users naturally flow from "show me status" to "show me the timeline for this check." The `grafana-investigate-alert` skill demonstrates that a single skill can cover fetch-then-query-then-analyze[3].
- A standalone `probes` skill would wrap a single command (`probes list`) with no workflow logic. This is too thin to justify its own skill. Probes are a supporting step in other workflows.
- Splitting CRUD operations (push vs pull vs delete) would fragment a natural "manage my checks" workflow.

### Trigger Separation Analysis

```
User says:                              Routes to:
"check my synthetic monitoring"     --> synth-check-status
"are my checks healthy"             --> synth-check-status
"show me check status"              --> synth-check-status
"what's the uptime for mysite"      --> synth-check-status
"show timeline for check 42"        --> synth-check-status

"why is check X failing"            --> synth-investigate-check
"diagnose this SM failure"          --> synth-investigate-check
"probe failures in us-east"         --> synth-investigate-check
"check is showing NODATA"           --> synth-investigate-check

"create a check for my API"         --> synth-manage-checks
"set up monitoring for example.com" --> synth-manage-checks
"pull my SM checks to disk"         --> synth-manage-checks
"push these check configs"          --> synth-manage-checks
"delete check 42"                   --> synth-manage-checks
```

No trigger phrase maps to more than one skill. The description negative-routing pattern ("For investigating failures, use synth-investigate-check instead") will handle edge cases.

### Workflow Pattern Mapping

| Skill | Workflow Pattern | Analog in Existing Skills |
|-------|-----------------|--------------------------|
| `synth-check-status` | **Linear Steps** (list -> status -> optional timeline) | `discover-datasources` |
| `synth-investigate-check` | **Decision-Branching** (triage -> conditional deep-dive) | `grafana-investigate-alert[5]` |
| `synth-manage-checks` | **Linear Steps** with decision table (choose type -> configure -> push) | `gcx` reference skill |

---

## 3. Skill #1: `synth-check-status`

### Specification

**Name:** `synth-check-status`

**Description (trigger text):**
```
Check the health and status of Grafana Synthetic Monitoring checks. Shows uptime, probe status, success rates, and timeline trends. Use when the user asks "are my checks healthy", "show SM status", "synthetic monitoring uptime", "check status", "probe success rate", or wants to see timeline graphs for specific checks. For investigating why a check is failing, use synth-investigate-check instead.
```
(296 chars)

**Allowed tools:** `[gcx, Bash]`

**Model:** (default -- no override needed)

### Core Workflow Steps

```
Step 1: List checks (orient)
  gcx synth checks list
  -> Shows ID, JOB, TARGET, TYPE

Step 2: Check status (overview or single)
  gcx synth checks status              # all checks
  gcx synth checks status <ID>         # single check
  -> Shows SUCCESS%, STATUS (OK/FAILING/NODATA), PROBES_UP

Step 3: (Conditional) Timeline for specific check
  If user asks about trends, or status shows FAILING:
  gcx synth checks timeline <ID> --window <window>
  -> Default: terminal line chart, one series per probe

Step 4: (Conditional) Detailed check config
  If user asks about configuration:
  gcx synth checks get <ID> -o wide
  -> Shows FREQ, TIMEOUT, PROBES, ENABLED
```

### Output Format Decisions

| Scenario | Format | Rationale |
|----------|--------|-----------|
| Status overview for user | default (table) | Human-readable summary |
| Status for agent processing | `-o json` | Extract specific fields |
| Timeline for user | default (graph) | Visual trend |
| Timeline data for agent | `-o json` | Time series data extraction |
| Check details | `-o wide` | Shows all columns inline |

### Interpretation Guidance (in SKILL.md)

Status values the skill must interpret[6]:
- `OK` (success >= 50%): Check is healthy
- `FAILING` (success < 50%): More than half of probes failing -- suggest investigation
- `NODATA`: No Prometheus data -- either check is new, datasource not configured, or metrics pipeline issue

Timeline interpretation[6]:
- Flat line at 1.0: All good
- Drops to 0.0: Probe-level failures -- note which probes
- Intermittent spikes to 0: Flapping -- may indicate borderline network/timeout issues

### Supporting Reference Files

None needed. This skill is straightforward enough to stay under 200 lines.

### Estimated Size

~150-180 lines including frontmatter, workflow, interpretation guidance, error handling, and output templates.

---

## 4. Skill #2: `synth-investigate-check`

### Specification

**Name:** `synth-investigate-check`

**Description (trigger text):**
```
Investigate why a Grafana Synthetic Monitoring check is failing or degraded. Diagnoses probe failures, identifies failure patterns (regional outage, SSL/TLS, DNS, timeout, content mismatch), and recommends next actions. Use when the user asks "why is this check failing", "diagnose SM failure", "probe errors", "check is NODATA", "synthetic monitoring down", or mentions a specific failing check. For just viewing status without investigation, use synth-check-status instead.
```
(468 chars)

**Allowed tools:** `[gcx, Bash]`

### Core Workflow Steps

```
Step 1: Identify the failing check
  gcx synth checks status -o json
  -> Filter for FAILING or NODATA checks
  If user named a specific check: gcx synth checks status <ID>

Step 2: Get check configuration
  gcx synth checks get <ID> -o json
  -> Extract: type, target, frequency, timeout, probes, alertSensitivity
  -> Note check type (determines what failure modes are relevant)

Step 3: Triage -- scope the failure
  gcx synth checks timeline <ID> --window 1h
  -> Show graph to user
  gcx synth checks timeline <ID> --window 1h -o json
  -> Analyze for agent:
     - All probes failing? -> Target/service issue
     - Subset of probes? -> Regional/network issue
     - Intermittent? -> Flapping/timeout issue
     - Recent onset? -> Change-related
     - NODATA? -> Metrics pipeline or check disabled

Step 4: (Conditional) Per-probe breakdown
  From the timeline JSON, identify which specific probes are failing.
  gcx synth probes list
  -> Map probe names to regions for geographic analysis

Step 5: (Conditional) Deeper metrics if datasource available
  # Success rate over longer window
  gcx query -d <datasource-uid> \
    -e 'avg_over_time(probe_success{job="<job>",instance="<target>"}[1h])' \
    --start now-24h --end now --step 5m -o graph

  # For HTTP checks: phase latency breakdown
  gcx query -d <datasource-uid> \
    -e 'probe_http_duration_seconds{job="<job>",instance="<target>",phase="resolve"}' \
    --start now-6h --end now --step 1m -o graph

Step 6: Provide diagnosis and next actions
  Based on failure pattern, provide:
  - Failure mode classification (from reference file)
  - Affected scope (all probes vs regional)
  - Timeline (when it started, ongoing vs resolved)
  - Suggested next actions
```

### Decision-Branching Logic

The skill uses the `grafana-investigate-alert` pattern of early exits and conditional depth:

```
Check status:
  If OK: "Check <name> is healthy (SUCCESS%). No issues detected." -> Stop
  If NODATA: Branch to NODATA diagnosis
    - Check if check is enabled
    - Check if datasource is configured
    - Suggest: "Check may be newly created or metrics pipeline may have issues"
  If FAILING: Continue to full investigation
```

### Failure Mode Reference

This skill needs a `references/failure-modes.md` file containing the 8 failure modes identified by the Grafana Synthetic Monitoring documentation and best practices[7][8]:

| Failure Mode | Signals | Likely Cause | Next Action |
|-------------|---------|-------------|-------------|
| Target down | All probes = 0, all regions | Service outage | Check service health, recent deployments |
| Regional/CDN | Subset of probes = 0 by region | CDN/network issue | Compare probe regions, check CDN status |
| SSL/TLS | HTTP checks, cert-related errors | Certificate expiry/mismatch | Check `probe_ssl_earliest_cert_expiry` |
| DNS resolution | DNS checks fail, HTTP slow on resolve phase | DNS misconfiguration | Check DNS records, TTL |
| Timeout | Intermittent 0s, slow target | Service degradation | Check `probe_http_duration_seconds` phases |
| Content/assertion | HTTP checks, body mismatch | Application change | Review check assertions vs current response |
| Private probe infra | Only private probes fail | Probe deployment issue | Check probe health, k8s pod status |
| Rate limiting | Periodic failures, 429 status | Target rate-limiting probes | Reduce frequency, allowlist probe IPs |

### Output Format Template

```
Check: <job> (<target>)
Type: <type> | Status: FAILING | Success: <N>%
Window: <timeframe analyzed>

[Timeline graph]

Failure pattern: <classification>
Affected probes: <list> (<N>/<total>)
Onset: <when> (<duration>)

Diagnosis:
<1-3 sentences explaining the likely cause>

Next actions:
- <action 1>
- <action 2>
- <action 3>
```

### Supporting Reference Files

- `references/failure-modes.md` -- The 8 failure mode patterns with signals, causes, and suggested actions
- `references/sm-promql-patterns.md` -- PromQL queries for deeper investigation (phase latency, SSL expiry, error rates by probe)

### Estimated Size

~300-350 lines for SKILL.md (decision-branching skills run longer). Plus ~150 lines across 2 reference files.

---

## 5. Skill #3: `synth-manage-checks`

### Specification

**Name:** `synth-manage-checks`

**Description (trigger text):**
```
Create, update, and manage Grafana Synthetic Monitoring checks. Handles check configuration (type, target, probes, frequency, alerts), push/pull for GitOps workflows, and deletion. Use when the user asks "create a check", "set up monitoring for", "push SM checks", "pull synthetic monitoring configs", "delete check", "configure probes for", or wants to manage check YAML files. For checking health status, use synth-check-status instead.
```
(431 chars)

**Allowed tools:** `[gcx, Bash, Read, Write, Edit]`

Note: This skill needs file tools (`Read`, `Write`, `Edit`) because it creates and modifies YAML check definition files on disk.

### Core Workflow Steps

```
Step 1: Understand the user's goal
  - Creating a new check? -> Go to Step 2
  - Updating existing checks? -> Go to Step 3
  - Pull/push (GitOps sync)? -> Go to Step 4
  - Deleting checks? -> Go to Step 5

Step 2: Create a new check
  2a. Determine check type (see references/check-types.md)
      - What is the target? URL, hostname, IP, domain?
      - What layer to test? Network (ping) -> DNS -> TCP -> HTTP -> Browser
  2b. List available probes
      gcx synth probes list
      -> Select minimum 3 geographically distributed probes
  2c. Build check YAML file
      [Template with K8s envelope structure]
  2d. Push the check
      gcx synth checks push <file> --dry-run   # preview first
      gcx synth checks push <file>              # apply

Step 3: Update existing checks
  3a. Pull current configs
      gcx synth checks pull -d ./checks
  3b. Modify YAML files as needed
  3c. Push changes
      gcx synth checks push ./checks/*.yaml --dry-run
      gcx synth checks push ./checks/*.yaml

Step 4: GitOps sync (pull/push)
  Pull: gcx synth checks pull -d <dir>
  Push: gcx synth checks push <files...>
  -> Note: push with numeric metadata.name = update, non-numeric = create

Step 5: Delete checks
  gcx synth checks delete <ID...>
  -> Without -f: prompts for confirmation
  -> With -f: skips confirmation
```

### Check YAML Template

The Synthetic Monitoring API follows a Kubernetes-compatible resource model[6]:

```yaml
apiVersion: syntheticmonitoring.ext.grafana.app/v1alpha1
kind: Check
metadata:
  namespace: default
spec:
  job: <descriptive-name>
  target: <url-or-host>
  frequency: 60000        # milliseconds (60s)
  timeout: 10000          # milliseconds (10s)
  enabled: true
  labels:
    - name: team
      value: <team-name>
  settings:
    http:                  # check type key
      method: GET
  probes:
    - Atlanta
    - Frankfurt
    - Singapore
  basicMetricsOnly: false
  alertSensitivity: ""    # "", "high", "medium", "low"
```

### Configuration Guidance (inline, brief)

Based on Grafana Synthetic Monitoring best practices[7]:

| Decision | Guidance |
|----------|---------|
| Frequency | Critical: 10000-60000ms (10-60s), Standard: 60000-300000ms (1-5min) |
| Timeout | Must be < frequency. HTTP: 10s default. Browser/scripted: 30-60s |
| Probes | Minimum 3 for meaningful alerting. Mix regions for coverage |
| alertSensitivity | `"high"` = >5% failure triggers alert, `"medium"` = >10%, `"low"` = >25% |
| basicMetricsOnly | `true` = lighter metrics footprint, `false` = full HTTP phase metrics |

### Supporting Reference Files

- `references/check-types.md` -- Decision tree for selecting check type, with per-type YAML examples based on Synthetic Monitoring API specification[6] (HTTP, Ping, DNS, TCP, Traceroute, MultiHTTP, Browser, Scripted)

### Estimated Size

~250-300 lines for SKILL.md. Plus ~200 lines for `references/check-types.md`.

---

## 6. Shared Patterns / CLAUDE.md Additions

### No CLAUDE.md Changes Needed

The existing CLAUDE.md already documents the `synth` provider's position in the architecture. Skills should not duplicate what CLAUDE.md already covers (per the anti-pattern: "never explain what gcx is").

### Shared Conventions Across All 3 Skills

These patterns should be consistent across all three skills, following established Claude Code skill best practices[2]:

**1. Core Principles block** (from `grafana-investigate-alert` pattern)[5]:
```markdown
## Core Principles

1. Use gcx commands -- do not call SM APIs directly
2. Trust the user's expertise -- no explaining what synthetic monitoring is
3. Use -o json for agent processing, default table/graph for user display
4. Show graphs for any time-series data -- operators think visually
```

**2. Error handling section** (collected, not interleaved):
```markdown
## Error Handling

- Config not set: "Run `gcx config set-context` with SM provider settings"
- No checks found: "No synthetic monitoring checks configured in this context"
- NODATA status: Check datasource configuration, check may be newly created
- Probe not found during push: "Probe name must match exactly (case-sensitive)"
```

**3. Agent mode awareness[9]:**
When running in agent mode (detected via environment), gcx defaults to JSON output. Skills should note this but not change their workflow -- the `-o json` / `-o graph` flags are explicit and override agent mode defaults.

**4. Datasource UID handling:**
All three skills may need the Prometheus datasource UID for `status`, `timeline`, or `query` commands. The resolution order is automatic (flag -> config -> auto-discovery). Skills should NOT ask the user for this unless auto-discovery fails.

### Cross-Skill References

The skills should use negative routing in their descriptions to direct to each other:
- `synth-check-status`: "For investigating failures, use synth-investigate-check instead"
- `synth-investigate-check`: "For just viewing status without investigation, use synth-check-status instead"
- `synth-manage-checks`: "For checking health status, use synth-check-status instead"

---

## 7. Implementation Priority

| Priority | Skill | Rationale | Effort |
|----------|-------|-----------|--------|
| **P0** | `synth-check-status` | Most common use case. Simple linear workflow. No reference files needed. Direct analog to SLO status pattern already proven. | Small (half day) |
| **P1** | `synth-investigate-check` | High value for incident response. Requires 2 reference files. Follows proven `grafana-investigate-alert` pattern. | Medium (1 day) |
| **P2** | `synth-manage-checks` | Important but less frequent. Requires 1 reference file. YAML template creation is the main complexity. | Medium (1 day) |

**Recommended implementation order[2]:** P0 -> P1 -> P2. Ship P0 first for immediate value, then layer investigation and management capabilities.

### File Tree (Final State)

```
.claude/skills/
  synth-check-status/
    SKILL.md                           (~150-180 lines)

  synth-investigate-check/
    SKILL.md                           (~300-350 lines)
    references/
      failure-modes.md                 (~100 lines)
      sm-promql-patterns.md            (~50 lines)

  synth-manage-checks/
    SKILL.md                           (~250-300 lines)
    references/
      check-types.md                   (~200 lines)
```

Total: 3 SKILL.md files + 3 reference files = 6 files, ~1050-1080 lines total.

---

## 8. Confidence Assessment

**Overall Confidence: 89% (High)**

| Section | Confidence | Rationale |
|---------|-----------|-----------|
| Skill decomposition (3 skills) | 92% | Strong convergence from skill architecture research, existing project patterns, and domain analysis. The only uncertainty is whether `synth-check-status` and `synth-investigate-check` should merge -- but the trigger separation and workflow shape differences strongly favor keeping them separate. |
| Skill #1: synth-check-status | 95% | Commands are fully implemented and documented with exact output formats. Direct analog to the SLO status pattern[9]. Lowest risk of any skill. |
| Skill #2: synth-investigate-check | 85% | Domain knowledge is solid (8 failure modes well-documented in Grafana Synthetic Monitoring documentation[7][8]). Uncertainty exists around which PromQL patterns the agent can actually run via `gcx query` -- this depends on what Prometheus datasource is available and what metrics exist. The `grafana-investigate-alert` skill provides a strong structural analog[5]. |
| Skill #3: synth-manage-checks | 87% | Push/pull/delete commands are well-documented. The check YAML template structure is known from the Synthetic Monitoring API[6]. Uncertainty exists in edge cases for complex check types (MultiHTTP, Browser, Scripted) where the settings map structure may have undocumented fields. |
| Shared patterns | 93% | Existing skill conventions are thoroughly analyzed across established Claude Code skill documentation[2]. High confidence in structural recommendations. |
| Implementation priority | 88% | Priority ordering is straightforward (simple -> complex, frequent -> rare). Effort estimates carry inherent uncertainty. |

### Areas of Uncertainty

- **Complex check type YAML structures**: The `settings` field is `map[string]any` -- we know the top-level key (e.g., `http`, `dns`, `browser`) from the Synthetic Monitoring API[6] but the inner structure for advanced types (MultiHTTP, Browser, Scripted) may have fields not captured in our research. The `references/check-types.md` file may need iteration after testing with real checks.

- **PromQL query availability**: The investigation skill assumes the agent can run `gcx query` against a Prometheus datasource that has SM metrics. If the datasource is not configured or metrics are not available, some investigation steps will fail gracefully but provide less value.

- **Probe name resolution edge cases**: Probe names are case-sensitive and must match exactly. The skill documents this, but we have not verified behavior when probe names contain special characters or when public probes are added/removed by Grafana.

- **Agent mode interaction with -o flags**: The existing code auto-selects JSON in agent mode. Skills explicitly set `-o` flags, which should override this. Verified in codebase (`BindFlags()` respects explicit `-o`), but worth confirming during implementation.

### Knowledge Gaps

- No existing SM-specific skill to compare against (these would be the first).
- No user feedback on what SM workflows are most requested (priority based on general patterns).
- Limited information on MultiHTTP and k6 Browser/Scripted check YAML examples in the wild.

---

## Synthesis Notes

- **Research domains covered:** 10 (4 codebase, 6 web)
- **Contradictions resolved:** 5 (see contradictions.md)
- **Key insight:** The SM provider's command structure (CRUD + status + timeline) directly mirrors the SLO provider[9], making the SLO skills a proven template. The main differentiator is that SM checks have per-probe granularity (geographic distribution) which adds a diagnostic dimension not present in SLO.
- **Structural decision:** 3 skills aligned to user intent (status/investigate/manage) rather than command grouping (checks/probes) or workflow phase (read/write). This matches how the existing `grafana-investigate-alert` skill is scoped -- by user intent, not by command.
- **Limitations:** Web research findings for probe management, product capabilities, and failure investigation were returned as summaries rather than full findings. The synthesis relies on the summary-level information provided, which was sufficient for skill design but means some domain details may be incomplete.

---

## References

[1] Claude Code Documentation: "Skill Architecture Principles."
    https://platform.claude.com/docs/en/agents-and-tools/agent-skills/best-practices
    (accessed 2026-03-07)

[2] Anthropic. "Claude Code Agent Skills: Best Practices and Design Patterns."
    https://platform.claude.com/docs/en/agents-and-tools/agent-skills/best-practices
    (accessed 2026-03-07)

[3] Claude Code Skills Repository: `grafana-investigate-alert` skill.
    `.claude/skills/grafana-investigate-alert/SKILL.md`
    (accessed 2026-03-07)

[4] gcx Project Documentation: "AGENTS.md — Agent Entry Point."
    `CLAUDE.md` - Project conventions for skill decomposition
    (accessed 2026-03-07)

[5] Claude Code Skills Repository: `grafana-investigate-alert` skill pattern.
    `.claude/skills/grafana-investigate-alert/SKILL.md`
    (accessed 2026-03-07)

[6] Grafana. "Synthetic Monitoring API Reference."
    https://github.com/grafana/synthetic-monitoring-api-go-client
    (accessed 2026-03-07)

[7] Grafana. "Synthetic Monitoring Best Practices."
    https://grafana.com/blog/2022/03/10/synthetic-monitoring-alerts-part-3-best-practices/
    (accessed 2026-03-07)

[8] Grafana. "Five Key Alerts for Synthetic Monitoring."
    https://grafana.com/blog/2022/01/11/five-essential-synthetic-monitoring-checks/
    (accessed 2026-03-07)

[9] gcx Project Documentation: "SLO Provider and Skills."
    `internal/providers/slo/` - SLO provider implementation
    (accessed 2026-03-07)

---

*Research conducted using Claude Code's multi-agent research system.*
*Session ID: research-e8431e5b-20260306-184525 | Generated: 2026-03-07*
