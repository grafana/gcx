---
type: feature-spec
title: "Stage 1: MVP plugin"
status: done
research: docs/research/2026-03-06-claude-code-plugin-for-gcx.md
parent: gcx-experiments-v27
beads_id: gcx-experiments-v28
created: 2026-03-06
---

# Stage 1: MVP plugin

## Problem Statement

Claude Code agents currently have no structured way to learn how to use gcx. The existing skill content (under `.claude/skills/`) is contributor-facing and contains four verified bugs -- wrong config paths, a fictional pipe command, incorrect JSON envelope paths, and an unverified flag. Any agent following these skills today will generate commands that fail silently or produce configuration errors.

The gcx project needs an installable Claude Code plugin that teaches agents to set up and use gcx correctly. This is Stage 1 of a 3-stage effort to make gcx the canonical agent interface for Grafana, superseding the separate mcp-grafana MCP server. Stage 1 focuses on three foundational skills (setup, datasource discovery, and alert investigation) plus fixing the four bugs, which unblocks all subsequent stages.

Current workaround: Users manually paste gcx commands into conversations or rely on the broken skill content, leading to trial-and-error debugging of malformed commands.

## Scope

### In Scope

- Scaffold the plugin directory structure, nested under `claude-plugin/` at the repo root (`claude-plugin/.claude-plugin/plugin.json`, `claude-plugin/agents/`, `claude-plugin/skills/`)
- Create `plugin.json` manifest with correct metadata
- Create `agents/grafana-debugger.md` as a stub (system prompt only, not a full workflow -- full agent comes in Stage 2)
- Fix all 4 bugs in existing skill content so corrected material can be reused
- Write `skills/setup-gcx/` skill from scratch using `agent-docs/config-system.md` as authoritative source
- Adapt `skills/explore-datasources/` skill from existing `.claude/skills/discover-datasources/` with bug fixes applied
- Adapt `skills/investigate-alert/` skill from existing `.claude/skills/grafana-investigate-alert/` with plugin-dev standards applied
- Create `references/configuration.md` from scratch (correct config paths, env vars, namespace resolution)
- Copy and verify `references/discovery-patterns.md` and `references/logql-syntax.md` from existing skill
- Verify the plugin loads with `claude --plugin-dir`
- Verify all three skills appear and auto-trigger on relevant user intents
- Validate each skill with `plugin-dev:skill-reviewer` (no major issues)
- Validate the plugin with `plugin-dev:plugin-validator` (passes without critical errors)

### Out of Scope

- **`debug-with-grafana` skill** -- Stage 2. Requires query-patterns.md rewrite and error-recovery.md (new content).
- **`manage-dashboards` skill** -- Stage 2. Requires selectors.md corrections and resource-model.md adaptation.
- **`monitor-slos` skill** -- Stage 2. Entirely new content requiring SLO provider source code analysis.
- **Hooks** (`hooks.json`, PostToolUse, SessionStart) -- Planned for v0.2.0.
- **Cross-agent packaging** (AGENTS.md for Codex, GEMINI.md, .cursorrules) -- Planned for v0.2.0.
- **MCP server component** -- Explicitly excluded from plugin architecture.
- **Plugin marketplace registration** -- Distribution setup is a separate task.
- **Changes to gcx source code** (GCX_AGENT_MODE, structured exit codes, etc.) -- Parallel workstream, not a dependency.
- **Fixing bugs in the `.claude/skills/` contributor-facing copies** -- The bug fixes go into the plugin copies only; the contributor-facing skills are a separate concern.

## Key Decisions

| Decision | Chosen | Rationale | Source |
|----------|--------|-----------|--------|
| Skills-first architecture (no MCP server) | Plugin uses skills + Bash tool, not an MCP server binary | gcx already outputs structured JSON (`-o json` on every read command); an MCP server would duplicate `internal/resources/` logic. Skills inject workflow knowledge; agents compose CLI commands. Zero additional process overhead. | Research report Section 1, Section 2 |
| Rewrite config content from scratch | Do not attempt to patch existing `configuration.md`; write new content from `agent-docs/config-system.md` | Existing config content uses a fictional `auth.type/auth.token` schema throughout. The bug is systematic (wrong struct hierarchy, wrong field names, wrong YAML layout). Patching would be error-prone; rewriting from the authoritative source is safer and faster. | Research report Section 5 (Bug 1), `agent-docs/config-system.md` data model |
| Adapt discover-datasources as-is with minimal edits | Copy existing `.claude/skills/discover-datasources/` content rather than rewriting | The discover-datasources skill has correct commands, correct jq paths, and no config path references. Only needs removal of any `gcx graph` pipe references and addition of cross-references. Highest-quality reusable asset. | Research report Section 4 reusability assessment |
| Stub the grafana-debugger agent (with investigate-alert reference) | Ship agent as a system prompt stub that references the investigate-alert skill for alert-specific workflows, but defers full debugging workflow to Stage 2 | The agent can now reference a concrete skill (investigate-alert) for alert investigation, making it partially functional. Full debugging workflow still depends on Stage 2 skills. | Staged delivery plan; investigate-alert skill provides alert-specific capability |
| Use plugin-dev meta-skills for implementation guidance and quality gates | Invoke `plugin-dev:plugin-structure` when scaffolding, `plugin-dev:skill-development` when writing skills, `plugin-dev:agent-development` when writing the agent stub, `plugin-dev:plugin-validator` and `plugin-dev:skill-reviewer` for final validation | Plugin-dev meta-skills encode best practices for Claude Code plugin authoring. Using them as quality gates ensures the plugin follows documented conventions without requiring the implementer to read all plugin documentation upfront. | Claude Code plugin-dev skill suite |
| Include investigate-alert in Stage 1 | Adapt `.claude/skills/grafana-investigate-alert/` into plugin as a third skill | The skill already exists, uses correct gcx commands (no Bug 1-4 patterns), and is production-quality (4-step workflow, error handling, output templates). Including it in Stage 1 adds a high-value operational skill with minimal adaptation effort. It also gives the grafana-debugger agent a concrete skill to reference for alert-specific workflows. | `.claude/skills/grafana-investigate-alert/SKILL.md` audit |

## Functional Requirements

### Plugin Structure

- FR-001: The implementation MUST create the following directory tree, with all plugin content nested inside a `claude-plugin/` subdirectory at the repository root:

```
claude-plugin/
    .claude-plugin/
        plugin.json
    agents/
        grafana-debugger.md
    skills/
        setup-gcx/
            SKILL.md
            references/
                configuration.md
        explore-datasources/
            SKILL.md
            references/
                discovery-patterns.md
                logql-syntax.md
        investigate-alert/
            SKILL.md
```

Nesting under `claude-plugin/` keeps all plugin content isolated from the rest of the repository (no `agents/` or `skills/` directories at the repo root), enables `git` change tracking as a coherent unit, and allows the plugin to be loaded with `claude --plugin-dir ./claude-plugin`.

- FR-002: `claude-plugin/.claude-plugin/plugin.json` MUST contain at minimum: `name` ("gcx"), `version` ("0.1.0"), `description`, and `keywords` array including "grafana", "observability", "prometheus", "loki".

- FR-003: `claude-plugin/.claude-plugin/plugin.json` MUST NOT contain `mcpServers`, `commands`, or `hooks` keys.

### Bug Fixes

- FR-004: All config path references in plugin content MUST use the correct schema from `agent-docs/config-system.md`. Specifically:
  - Token: `gcx config set contexts.<name>.grafana.token <value>` (NOT `contexts.<name>.auth.type token` / `contexts.<name>.auth.token`)
  - User: `gcx config set contexts.<name>.grafana.user <value>` (NOT `contexts.<name>.auth.username`)
  - Org ID: `gcx config set contexts.<name>.grafana.org-id <value>` (NOT `contexts.<name>.namespace`)
  - Stack ID: `gcx config set contexts.<name>.grafana.stack-id <value>`

- FR-005: All graph output references MUST use `-o graph` as a codec on the `query` command. The plugin MUST NOT contain any reference to `gcx graph` as a standalone subcommand or a pipe target (`| gcx graph`).

- FR-006: All `datasources list` jq examples MUST use the correct JSON envelope path `.datasources[0].uid` (NOT `.[0].uid`).

- FR-007: The plugin MUST NOT reference the `--all-versions` flag.

### setup-gcx Skill

- FR-008: `skills/setup-gcx/SKILL.md` MUST include YAML frontmatter with `name` and `description` fields. The `description` MUST mention setup, configuration, authentication, connection, and first-time use.

- FR-009: The skill MUST document three configuration paths:
  - **Path A -- Grafana Cloud**: Set server URL, set token via `grafana.token`, use-context, config check. MUST note that namespace (stack-id) is auto-discovered from the server URL.
  - **Path B -- On-premise**: Set server URL, set token (or user/password via `grafana.user`/`grafana.password`), set org-id via `grafana.org-id`, use-context, config check.
  - **Path C -- Environment variables (CI/CD)**: `GRAFANA_SERVER`, `GRAFANA_TOKEN`, `GRAFANA_ORG_ID`/`GRAFANA_STACK_ID`.

- FR-010: The skill MUST include setting default datasources (`default-prometheus-datasource`, `default-loki-datasource`) to avoid repeated `-d` flag usage.

- FR-011: The skill MUST include a troubleshooting section covering: config check failures, 401/403 auth errors, connection refused/timeout, and namespace resolution issues.

- FR-012: `skills/setup-gcx/references/configuration.md` MUST be written from scratch using `agent-docs/config-system.md` as the sole authoritative source. It MUST NOT be adapted from `.claude/skills/gcx/references/configuration.md`.

- FR-013: `references/configuration.md` MUST document: correct `config set` paths for all fields, environment variable names and precedence, config file location, namespace resolution logic, and multi-context management.

### explore-datasources Skill

- FR-014: `skills/explore-datasources/SKILL.md` MUST be adapted from `.claude/skills/discover-datasources/SKILL.md`. The adaptation MUST preserve the existing four-step structure, all examples, troubleshooting entries, and Advanced Usage section.

- FR-015: `skills/explore-datasources/SKILL.md` MUST include YAML frontmatter with `name` and `description` fields. The `description` MUST mention datasource discovery, metrics, labels, log streams, and datasource UIDs.

- FR-016: `skills/explore-datasources/SKILL.md` MUST add a cross-reference to the `setup-gcx` skill.

- FR-017: `skills/explore-datasources/references/discovery-patterns.md` MUST be copied from `.claude/skills/discover-datasources/references/discovery-patterns.md` after verifying it contains no Bug 1-4 content.

- FR-018: `skills/explore-datasources/references/logql-syntax.md` MUST be copied from `.claude/skills/discover-datasources/references/logql-syntax.md` after verifying it contains no Bug 1-4 content.

### investigate-alert Skill

- FR-031: `skills/investigate-alert/SKILL.md` MUST be adapted from `.claude/skills/grafana-investigate-alert/SKILL.md`. The adaptation MUST preserve the existing 4-step workflow structure (Verify Context, Get Alert Details, Full Investigation, Surface Resources), output format templates, and error handling section.

- FR-032: `skills/investigate-alert/SKILL.md` MUST include YAML frontmatter with `name` and `description` fields. The `description` MUST mention alert investigation, alert firing, alert state, and Grafana alerts.

- FR-033: The adapted skill MUST contain zero instances of Bug 1-4 patterns. Specifically: no `auth.type`/`auth.token` config paths, no `| gcx graph` pipes, no bare-array jq paths on datasource list output, no `--all-versions` flag.

- FR-034: The adapted skill MUST add a cross-reference to the `setup-gcx` skill for cases where gcx is not configured.

### grafana-debugger Agent Stub

- FR-019: `agents/grafana-debugger.md` MUST contain YAML frontmatter with `name` ("grafana-debugger") and `description` fields. The description MUST mention diagnosing application issues, errors, latency, and service degradation using Grafana observability data.

- FR-020: The agent body MUST include a brief system prompt establishing the agent's diagnostic approach: discover datasources, confirm scraping, query error rates, correlate logs, summarize findings.

- FR-021: The agent MUST note that `-o json` must always be used for machine-parseable output and datasource UIDs (not names) must be used in queries.

- FR-022: The agent MUST note that if gcx is not configured, it should guide the user through setup first.

### Plugin Loading

- FR-023: The plugin MUST load successfully when invoked with `claude --plugin-dir ./claude-plugin`.

- FR-024: All three skills (`setup-gcx`, `explore-datasources`, `investigate-alert`) MUST appear in the skill list when the plugin is loaded.

- FR-025: The `setup-gcx` skill MUST auto-trigger when a user asks about configuring gcx or setting up a Grafana connection.

- FR-026: The `explore-datasources` skill MUST auto-trigger when a user asks about available datasources, metrics, or Grafana data inventory.

- FR-035: The `investigate-alert` skill MUST auto-trigger when a user asks about Grafana alerts, why an alert is firing, or alert investigation.

### Plugin-Dev Meta-Skill Quality Gates

- FR-027: The implementer MUST invoke `plugin-dev:plugin-structure` before writing `plugin.json` to ensure the manifest follows current Claude Code plugin conventions.

- FR-028: Each skill (`setup-gcx`, `explore-datasources`, `investigate-alert`) MUST be reviewed using `plugin-dev:skill-reviewer` after initial authoring. The review MUST find no major issues before the skill is considered complete.

- FR-029: The `agents/grafana-debugger.md` stub MUST be written following `plugin-dev:agent-development` guidance (frontmatter fields, description quality, tool configuration).

- FR-030: The completed plugin MUST be validated with `plugin-dev:plugin-validator`. Validation MUST pass without critical errors before the implementation is considered done.

## Acceptance Criteria

- GIVEN the plugin files exist at the repository root
  WHEN I inspect the directory structure
  THEN `claude-plugin/.claude-plugin/plugin.json` exists and is valid JSON with `name`, `version`, `description`, and `keywords` fields; `claude-plugin/agents/grafana-debugger.md` exists with YAML frontmatter; `claude-plugin/skills/setup-gcx/SKILL.md` exists with YAML frontmatter; `claude-plugin/skills/explore-datasources/SKILL.md` exists with YAML frontmatter; `claude-plugin/skills/investigate-alert/SKILL.md` exists with YAML frontmatter; all referenced `references/` files exist inside `claude-plugin/`.

- GIVEN all plugin content files
  WHEN I search for `auth.type`, `auth.token`, `auth.username`, `auth.password`, or `contexts.<name>.namespace` as a config set target
  THEN zero matches are found. All config commands use the `grafana.token`, `grafana.user`, `grafana.password`, `grafana.org-id`, `grafana.stack-id` schema.

- GIVEN all plugin content files
  WHEN I search for `| gcx graph` or `gcx graph` as a standalone command
  THEN zero matches are found. Graph output is only referenced as `-o graph` on the `query` command.

- GIVEN all plugin content files
  WHEN I search for `jq '.[0].uid'` or similar bare-array jq paths for datasource list output
  THEN zero matches are found. Datasource list jq examples use `.datasources[0].uid`.

- GIVEN all plugin content files
  WHEN I search for `--all-versions`
  THEN zero matches are found.

- GIVEN `skills/setup-gcx/SKILL.md`
  WHEN I read the skill content
  THEN it contains a Grafana Cloud path using `grafana.token`, an on-premise path using `grafana.user`/`grafana.org-id`, and an environment variable path using `GRAFANA_SERVER`/`GRAFANA_TOKEN`/`GRAFANA_ORG_ID`.

- GIVEN `skills/explore-datasources/SKILL.md`
  WHEN I compare it to `.claude/skills/discover-datasources/SKILL.md`
  THEN all four steps, all four examples, troubleshooting entries, and the Advanced Usage section are present. No `| gcx graph` references exist. A cross-reference to `setup-gcx` is present.

- GIVEN the complete plugin at the repository root
  WHEN I run `claude --plugin-dir ./claude-plugin`
  THEN the plugin loads without errors, and `setup-gcx`, `explore-datasources`, and `investigate-alert` appear as available skills.

- GIVEN the plugin is loaded
  WHEN I say "help me set up gcx"
  THEN the `setup-gcx` skill is triggered.
  WHEN I say "what datasources does my Grafana have"
  THEN the `explore-datasources` skill is triggered.
  WHEN I say "why is this alert firing"
  THEN the `investigate-alert` skill is triggered.

- GIVEN `skills/investigate-alert/SKILL.md`
  WHEN I compare it to `.claude/skills/grafana-investigate-alert/SKILL.md`
  THEN the 4-step workflow, output format templates, and error handling section are preserved. A cross-reference to `setup-gcx` is present.

- GIVEN `skills/investigate-alert/SKILL.md` is complete
  WHEN I run `plugin-dev:skill-reviewer` against it
  THEN the review finds no major issues.

- GIVEN `skills/setup-gcx/references/configuration.md`
  WHEN I compare the config set commands to the data model in `agent-docs/config-system.md`
  THEN every config path matches the actual struct hierarchy: `contexts.<name>.grafana.server`, `contexts.<name>.grafana.token`, `contexts.<name>.grafana.user`, `contexts.<name>.grafana.password`, `contexts.<name>.grafana.org-id`, `contexts.<name>.grafana.stack-id`, `contexts.<name>.grafana.tls.*`.

- GIVEN `skills/setup-gcx/SKILL.md` is complete
  WHEN I run `plugin-dev:skill-reviewer` against it
  THEN the review finds no major issues (no missing required frontmatter fields, no description clarity failures, no structural problems that would prevent auto-triggering).

- GIVEN `skills/explore-datasources/SKILL.md` is complete
  WHEN I run `plugin-dev:skill-reviewer` against it
  THEN the review finds no major issues.

- GIVEN the completed plugin at `claude-plugin/`
  WHEN I run `plugin-dev:plugin-validator`
  THEN validation passes without critical errors (valid manifest structure, all referenced files present, no schema violations).

## Negative Constraints

- NEVER include an MCP server (`mcpServers` key in plugin.json, or any server binary).
- NEVER include hooks (`hooks` key in plugin.json, or any hooks.json file).
- NEVER include slash commands (`commands/` directory).
- DO NOT reference `gcx resources edit` (interactive TTY command, unusable by agents).
- DO NOT reference `gcx graph` as a standalone subcommand or pipe target.
- DO NOT include contributor-facing content (add-provider skill, dev scaffold, dev import commands).
- DO NOT reference the internal selector resolution algorithm (parse -> discover -> match -> resolve).
- DO NOT reference concurrency tuning (`--max-concurrent`, errgroup internals).
- DO NOT reference `GCX_AGENT_MODE` (documented as planned but not yet implemented).
- The `references/configuration.md` MUST NOT be derived from `.claude/skills/gcx/references/configuration.md`.

## Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Auto-triggering selects wrong skill for ambiguous queries | User gets loaded with irrelevant context, wastes tokens | Write skill descriptions with minimal overlap; test with 5-10 ambiguous prompts during verification |
| Adapted discover-datasources content contains undiscovered bugs beyond the 4 known ones | Agent follows incorrect commands at runtime | Manually trace every `gcx` command in the plugin through the codebase to verify correctness |
| `references/configuration.md` written from agent-docs may have stale content | Config commands in plugin are wrong despite using "authoritative" source | Cross-reference config-system.md entries against `internal/config/types.go` struct tags |
| Plugin directory `.claude-plugin/` conflicts with repo's `.claude/` directory | Ambiguous skill loading behavior | Verify with `claude --plugin-dir` that only plugin skills are loaded from plugin path |
| Claude Code plugin system changes behavior between versions | Plugin stops loading or auto-triggering breaks | Pin to documented plugin.json schema; avoid undocumented behavior |

## Open Questions

- [NEEDS VERIFICATION]: Does the `--all-versions` flag actually exist on `gcx resources pull`? Run `gcx resources pull --help` to verify. If confirmed, add to manage-dashboards skill in Stage 2.

- [NEEDS VERIFICATION]: How accurately does Claude Code auto-trigger skills based on `description` field matching? Requires manual testing with a matrix of user prompts before shipping.

- [RESOLVED]: Should the `grafana-debugger` agent stub reference specific skills by name? — Yes, it can now reference `investigate-alert` for alert-specific workflows. General debugging skills remain Stage 2.

- [UNCERTAIN]: Should `explore-datasources/SKILL.md` explicitly fix Bug 3 even if the existing discover-datasources skill doesn't have it? Verify discover-datasources jq examples before copying.

- [DESIGN]: What should the `allowed-tools` frontmatter field contain? gcx is invoked via Bash, not a named tool. Verify the field's semantics in Claude Code plugin documentation.
