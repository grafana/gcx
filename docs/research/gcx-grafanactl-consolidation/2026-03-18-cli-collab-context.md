# CLI Collab Context — gcx vs grafanactl Feature Vote

*2026-03-18 | Research for [[CLI collab follow-up — gcx vs grafanactl feature vote]]*
*Sources: [Grafana Cloud CLI plans doc](https://docs.google.com/document/d/1DZl2IL7-dgTIoLlhufAn13mbt11WkafoM8aZxbmqYlc/edit), grafana/grafanactl-experiments, grafana/grafana-cloud-cli*

---

## What Changed Since the March 13 Executive Brief

The [executive brief](2026-03-13-executive-brief-grafanactl-vs-gcx.md) listed "query engine (PromQL/LogQL/Pyroscope with terminal graph rendering)" as grafanactl's primary moat. That's **no longer accurate**:

**gcx now has full query capability:**
- `gcx metrics query <promql>` — full PromQL (instant + range)
- `gcx logs query <logql>` — full LogQL, plus labels/series/stats/volume/patterns
- `gcx traces get` — Tempo v2 API + LLM trace format
- `gcx profiles query` — Pyroscope
- `gcx datasources query` — arbitrary datasource query via `/api/ds/query`
- `gcx datasources introspect` — inspect datasource schema (metrics, labels)
- `gcx telemetry analyze/diff` — cross-signal analysis, before/after comparison
- `gcx usage` — cost attribution by metric/dashboard/rule (NEW, merged 2026-03-18)

**What grafanactl still has that gcx doesn't:**
- Linter (Rego/OPA) with PromQL/LogQL validators + custom rule authoring, testing, catalog
- Live dev server (`grafanactl serve`) with file-watcher + WebSocket LiveReload
- Code generation (`grafanactl dev generate`) for typed Go resource stubs
- K8s-native client (`k8s.io/client-go`) → watch, discovery, dynamic schema registration via app platform
- Terminal graph rendering in agent-first design (PR #35 adds `graph` + `wide` output modes for SLO/Synth)

---

## Combined Feature List (Tab 3, deduplicated)

Features from the joint planning doc, cleaned up and consolidated:

### Query & Visualization

| Feature | gcx status | grafanactl status |
|---------|-----------|------------------|
| PromQL / LogQL / Pyroscope queries | ✅ full | ✅ full (refactor in PR #36) |
| ASCII graph output (`-o graph`) | partial (telemetry rendering) | PR #35 adds `graph`/`wide` modes |
| Dashboard snapshots via render | ❌ | PR #34 merged (snapshot cmd) |
| Image interpretation (AI reads graphs) | ❌ | ❌ |

### Operations & Resource Management

| Feature | gcx status | grafanactl status |
|---------|-----------|------------------|
| Full Grafana Cloud resource CRUD | ✅ 70+ types | ~25 types (K8s API + 3 providers) |
| `push/pull` (apply/export/diff) | ✅ apply/export/diff | ✅ push/pull |
| Plan mode (tf-style dry run) | WIP in doc | ❌ |
| Dynamic schema registry (app platform) | ❌ | Partially (K8s discovery) |
| Linter (Rego/OPA + dashboard rules) | ❌ | ✅ unique |
| `$CLI dev` (FoundationSDK + serve + lint) | ❌ | ✅ serve, generate, lint |

### Agent Experience

| Feature | gcx status | grafanactl status |
|---------|-----------|------------------|
| Agent-ready JSON output + auto-detection | ✅ JSON envelope | ✅ TTY/pipe detection (PR #31) |
| MCP server | ✅ `gcx mcp serve` | ❌ |
| Annotated command surface (token costs, examples) | ✅ `gcx commands` JSON tree | ❌ |
| Skills plugin distribution | ✅ 11 skills + skills.sh | ✅ 9 Claude Code skills |
| Grafana-debugger agent persona | ❌ | ✅ |
| Schema/example introspection per resource | ✅ `gcx schema/example` | ❌ |

### Developer & Setup UX

| Feature | gcx status | grafanactl status |
|---------|-----------|------------------|
| Default datasource / directory pinning | partial (init flow) | ✅ per-context config |
| Multi-stack context switching | ✅ `gcx context` | ✅ `--context` flag |
| Keychain support | PR #73 open | ❌ |
| Tiered credential scopes | ✅ | ❌ |
| Backward compat / migration path | N/A | ✅ existing CI users |
| Error handling (unavailable Grafana, 503) | ✅ `gcx doctor` | partial |

---

## GrafanaCon Context

From the doc, explicit GrafanaCon release goals:
- Joined forces with grafanactl folks / merge with Assistant CLI
- OSS Public Preview checklist
- User-friendly OAuth authZ flow (`gcx new` onboarding)
- 5 min talk track + 2 min demo script
- Product marketing messaging
- Verified command surface + skill quality
- Test infra for API drift detection
- `--help` docs fleshed out with examples

**Non-goals**: Support for pre-2026 releases of self-hosted LGTM or Grafana Cloud.

---

## Strategic Framing

gcx is mature on breadth + agent protocols (MCP, JSON envelope, A2A, skills). grafanactl is mature on operational depth (query, lint, dev loop, K8s-native). The combined tool needs both, but with 5 weeks to GrafanaCon:

- Can't rebuild everything from scratch
- Must be agent-first, agent-only is OK initially
- Must claim "all of Grafana Cloud" to justify the merge story
- Demo needs to show something *no other Grafana tooling* can do

The dynamic schema registry (app platform auto-discovery) + linter (quality gates) + closed feedback loop (generate → lint → preview → push → verify) is the unique pitch — gcx has breadth but no closed loop; Grafana Assistant has investigation but no write path.
