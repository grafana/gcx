---
type: task
project: grafanactl
area: Grafana Labs
okr: O2
status: open
priority: P1
beads_id: ""
tags: [grafanactl, cli, spike]
---

# Spike gcx↔grafanactl consolidation directions

**OKR:** O2 — GrafanaCON 2026 agentic CLI roadmap
**Status:** Open

## Goal

Try both consolidation directions from the CLI collab sync (2026-03-19 with [[Ward Bekker]] and [[Artur Wierzbicki]]) to see which sticks:

- **Direction A:** Port `gcx` resources into `grafanactl` as providers
	- https://gist.github.com/wardbekker/0b5914b6871a9248a1abab8c5ea90954 - approach
- **Direction B:** Move `grafanactl` dynamic resource registration into `gcx`
	- https://github.com/grafana/grafana-cloud-cli/compare/main...feat/k8s-migration - PoC
	- https://gist.github.com/wardbekker/51392518685d2b7f8e2e36735d12450a - approach
The outcome should be a clear recommendation (with rationale) for the combined CLI architecture, to inform the GrafanaCON roadmap and the 12-month deprecation plan.

## Context

- CLI naming favored: **grot** (rejected anything `-ctl`)
- Decision from sync: adopt dynamic schema registration model long-term
- Single CLI absorbing both Assistant CLI and grafanactl; 12-month deprecation policy
- Ward is handling GrafanaCON keynote/demo — this spike feeds the architecture decision

## Acceptance Criteria

- [ ] Both directions spiked (even if just partially)
- [ ] Notes captured on what worked, what didn't, and effort estimate per direction
- [ ] Clear recommendation written up for the team

## Related

- [[grafanactl agentic evolution]] — parent theme
- [[Grafana Labs/1-1s/Artur]] — discussed consolidation approach
- [[Fabrizia]] / Ward Bekker — CLI collab sync 2026-03-19
