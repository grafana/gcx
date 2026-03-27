---
type: feature-tasks
title: "Stage 1: MVP plugin"
status: draft
spec: docs/specs/feature-v28/spec.md
plan: docs/specs/feature-v28/plan.md
created: 2026-03-06
---

# Implementation Tasks

## Dependency Graph

```
Wave 1: Scaffold
  T1: plugin scaffold + plugin.json
        |
        +---------------------------+------+------+
        |                    |      |      |      |
Wave 2+3: (all parallel from T1)    |      |      |
  T2: setup-gcx       T3:    T4:    T4b:   T5: grafana-debugger stub
  (skill from scratch)       conf.md explore invest. (can reference investigate-alert)
        |                    |      |      |      |
        +---------------------------+------+------+
        |
Wave 4: Quality Gates (depends on T2, T3, T4, T4b, T5)
  T6: plugin-dev validation + bug verification
```

---

## Wave 1: Plugin Scaffold

### T1: Create plugin directory structure and manifest
**Priority**: P0
**Effort**: Small
**Depends on**: none
**Type**: chore
**FRs**: FR-001, FR-002, FR-003, FR-027

Create the plugin directory tree and write `plugin.json`. Follow `plugin-dev:plugin-structure` guidance for manifest schema, auto-discovery rules, and directory conventions.

**Procedure:**

1. **Load plugin-dev:plugin-structure guidance.** Key conventions to follow:
   - Manifest lives at `.claude-plugin/plugin.json` inside the plugin root.
   - Auto-discovery: Claude Code auto-discovers `.md` files in `agents/`, `skills/`, and `commands/` directories.
   - Use `${CLAUDE_PLUGIN_ROOT}` for portable path references within the plugin.
   - All identifiers use kebab-case naming.
   - Required manifest fields: `name`, `version`, `description`.
   - Optional but recommended: `keywords`, `author`, `repository`, `license`.
2. Create all directories:
   - `claude-plugin/.claude-plugin/`
   - `claude-plugin/agents/`
   - `claude-plugin/skills/setup-gcx/references/`
   - `claude-plugin/skills/explore-datasources/references/`
   - `claude-plugin/skills/investigate-alert/`
3. Write `claude-plugin/.claude-plugin/plugin.json` with:
   ```json
   {
     "name": "gcx",
     "version": "0.1.0",
     "description": "Debug, explore, and instrument with Grafana using gcx CLI",
     "keywords": ["grafana", "observability", "monitoring", "prometheus", "loki", "dashboards"],
     "author": { "name": "Grafana Labs" },
     "license": "Apache-2.0"
   }
   ```
4. Verify JSON is valid and contains no `mcpServers`, `commands`, or `hooks` keys.

**Deliverables:**
- `claude-plugin/.claude-plugin/plugin.json`
- Directory tree: `claude-plugin/agents/`, `claude-plugin/skills/setup-gcx/references/`, `claude-plugin/skills/explore-datasources/references/`, `claude-plugin/skills/investigate-alert/`

**Acceptance criteria:**
- GIVEN the repository root WHEN I inspect `claude-plugin/.claude-plugin/plugin.json` THEN it is valid JSON with `name` = "gcx", `version` = "0.1.0", `description` present, `keywords` containing "grafana", "observability", "prometheus", "loki"
- GIVEN the plugin.json file WHEN I search for `mcpServers`, `commands`, or `hooks` keys THEN zero matches are found
- GIVEN the directory tree WHEN I list all paths THEN `claude-plugin/agents/`, `claude-plugin/skills/setup-gcx/references/`, `claude-plugin/skills/explore-datasources/references/`, `claude-plugin/skills/investigate-alert/` all exist

---

## Wave 2: Content (parallel tasks)

### T2: Write setup-gcx skill from scratch
**Priority**: P0
**Effort**: Medium
**Depends on**: T1
**Type**: task
**FRs**: FR-004, FR-005, FR-007, FR-008, FR-009, FR-010, FR-011

Write `claude-plugin/skills/setup-gcx/SKILL.md` entirely from scratch using `agent-docs/config-system.md` as the authoritative source for all config paths. Follow the `plugin-dev:skill-development` 6-step process and validation checklist.

**Plugin-dev:skill-development guidance (6-step process):**

1. **Understand** -- The skill teaches agents to configure gcx for first-time use. Target user intents: "set up gcx", "configure Grafana connection", "authenticate with Grafana", "create a gcx context".
2. **Plan reusable contents** -- One reference file: `references/configuration.md` (T3). No scripts or assets needed.
3. **Create structure** -- `skills/setup-gcx/SKILL.md` + `references/configuration.md`.
4. **Edit** -- Write in this order: references first (T3), then SKILL.md (this task).
   - **YAML frontmatter**: `name` + `description`. Description must be third-person with specific trigger phrases: "Set up and configure gcx for connecting to Grafana instances. Use when the user needs to configure authentication, create contexts, troubleshoot connection issues, or set up gcx for the first time."
   - **Body target**: 1,500-2,000 words. Use imperative/infinitive form ("Configure the server URL", "Set the authentication token"). Do NOT use first person or second person in instructions.
   - **Progressive disclosure**: SKILL.md contains the workflow (three config paths, default datasources, troubleshooting). Detailed config path reference lives in `references/configuration.md`.
   - **Reference bundled resources explicitly**: Include `See references/configuration.md for the complete list of config set paths and environment variable precedence.`
5. **Validate** -- Run the validation checklist (see below).
6. **Iterate** -- Address any checklist failures before marking complete.

**Validation checklist (from plugin-dev:skill-development):**
- [ ] SKILL.md has valid YAML frontmatter (opening and closing `---`)
- [ ] `name` and `description` fields present in frontmatter
- [ ] Description uses third person with specific trigger phrases
- [ ] Body 1,500-2,000 words (hard max 2,500)
- [ ] Writing uses imperative/infinitive form
- [ ] Progressive disclosure: workflow in SKILL.md, detailed data in references/
- [ ] All file references (`references/configuration.md`) actually exist
- [ ] Examples are complete and working (config commands match agent-docs/config-system.md)

**Procedure:**

1. Read `agent-docs/config-system.md` (key struct: `contexts.<name>.grafana.{server,token,user,password,org-id,stack-id}`).
2. Write SKILL.md with YAML frontmatter (`name`, `description`).
3. Document three configuration paths:
   - Path A (Grafana Cloud): `grafana.token`, auto-discovered stack-id
   - Path B (On-premise): `grafana.user`/`grafana.password`, `grafana.org-id`
   - Path C (Environment variables): `GRAFANA_SERVER`, `GRAFANA_TOKEN`, `GRAFANA_ORG_ID`/`GRAFANA_STACK_ID`
4. Include default datasource configuration (`default-prometheus-datasource`, `default-loki-datasource`).
5. Include troubleshooting section: config check failures, 401/403, connection refused/timeout, namespace resolution.
6. Add explicit cross-reference to `references/configuration.md` for detailed paths.
7. Verify zero instances of Bug 1-4 patterns in the output file.
8. Run the validation checklist above; address any failures.

**Deliverables:**
- `claude-plugin/skills/setup-gcx/SKILL.md`

**Acceptance criteria:**
- GIVEN `claude-plugin/skills/setup-gcx/SKILL.md` WHEN I read the frontmatter THEN `name` and `description` fields are present; description mentions setup, configuration, authentication, connection, and first-time use
- GIVEN the skill description WHEN I evaluate it against plugin-dev:skill-development standards THEN it uses third person, contains specific trigger phrases, and is between 1-3 sentences
- GIVEN the skill body WHEN I count words THEN it is between 1,500 and 2,500 words (target 1,500-2,000)
- GIVEN the skill body WHEN I check writing style THEN imperative/infinitive form is used consistently (not first-person or second-person)
- GIVEN the skill content WHEN I search for `auth.type`, `auth.token`, `auth.username`, `contexts.<name>.namespace` as config set targets THEN zero matches are found
- GIVEN the skill content WHEN I search for `gcx graph` as standalone command or pipe target THEN zero matches are found
- GIVEN the skill content WHEN I search for `--all-versions` THEN zero matches are found
- GIVEN the skill content WHEN I read it THEN all three config paths (Cloud, on-prem, env vars) are documented
- GIVEN the skill content WHEN I read it THEN default datasource configuration and troubleshooting sections are present
- GIVEN the skill content WHEN I search for `references/configuration.md` THEN at least one explicit cross-reference is found

---

### T3: Write configuration.md reference from scratch
**Priority**: P0
**Effort**: Medium
**Depends on**: T1
**Type**: task
**FRs**: FR-004, FR-012, FR-013

Write `claude-plugin/skills/setup-gcx/references/configuration.md` from scratch using `agent-docs/config-system.md` as the sole authoritative source. This file MUST NOT be derived from `.claude/skills/gcx/references/configuration.md` (which contains Bug 1 throughout).

This is the "references first" step of the plugin-dev:skill-development progressive disclosure pattern -- detailed data goes in reference files so SKILL.md can stay lean (1,500-2,000 words).

**Procedure:**

1. Read `agent-docs/config-system.md` data model section.
2. Extract all `config set` paths from the struct hierarchy:
   - `contexts.<name>.grafana.server`
   - `contexts.<name>.grafana.token`
   - `contexts.<name>.grafana.user`
   - `contexts.<name>.grafana.password`
   - `contexts.<name>.grafana.org-id`
   - `contexts.<name>.grafana.stack-id`
   - `contexts.<name>.grafana.tls.insecure-skip-verify`
   - `contexts.<name>.grafana.tls.ca-data`
   - `contexts.<name>.grafana.tls.cert-data`
   - `contexts.<name>.grafana.tls.key-data`
   - `contexts.<name>.default-prometheus-datasource`
   - `contexts.<name>.default-loki-datasource`
3. Document environment variable names and precedence (env vars override current context only).
4. Document config file location priority (5 levels).
5. Document namespace resolution logic (stack-id auto-discovery, org-id fallback).
6. Document multi-context management (use-context, list-contexts, --context flag).
7. Cross-reference every path against `agent-docs/config-system.md` struct hierarchy to verify correctness.
8. Spot-check key paths against `internal/config/types.go` struct tags to verify config-system.md itself is not stale (per spec Risk #3 mitigation).

**Deliverables:**
- `claude-plugin/skills/setup-gcx/references/configuration.md`

**Acceptance criteria:**
- GIVEN `claude-plugin/skills/setup-gcx/references/configuration.md` WHEN I compare every `config set` path to the data model in `agent-docs/config-system.md` THEN every path matches the actual struct hierarchy
- GIVEN the file WHEN I search for `auth.type`, `auth.token`, `auth.username`, `namespace` as a config set target THEN zero matches are found
- GIVEN the file WHEN I read it THEN it documents: config set paths for all fields, environment variable names and precedence, config file location, namespace resolution logic, and multi-context management
- GIVEN the file WHEN I compare it to `.claude/skills/gcx/references/configuration.md` THEN the content is structurally different (not a patch of the old file)

---

### T4: Adapt explore-datasources skill and copy references
**Priority**: P0
**Effort**: Medium
**Depends on**: T1
**Type**: task
**FRs**: FR-005, FR-006, FR-007, FR-014, FR-015, FR-016, FR-017, FR-018

Adapt `skills/explore-datasources/SKILL.md` from the existing `.claude/skills/discover-datasources/SKILL.md`. Copy and fix the two reference files. This task owns all Bug 2 and Bug 3 fixes in copied content. Follow `plugin-dev:skill-development` guidance for the adaptation.

**Plugin-dev:skill-development guidance for adaptation:**

Since this is an adaptation (not a from-scratch write), the 6-step process maps as follows:
1. **Understand** -- The skill teaches agents to discover datasources, metrics, labels, and log streams. Source material is high quality (90%+ correct).
2. **Plan** -- Two reference files: `discovery-patterns.md` (needs Bug 2 + Bug 3 fixes), `logql-syntax.md` (verbatim copy).
3. **Create structure** -- Already created in T1.
4. **Edit** -- Adapt SKILL.md with these plugin-dev standards:
   - **YAML frontmatter**: `name` ("explore-datasources") + `description` (third-person, trigger phrases: "Discover what datasources, metrics, labels, and log streams are available in a Grafana instance. Use when the user asks what data exists, what metrics are available, what services are being monitored, or needs to find a datasource UID.")
   - **Body**: Verify word count is within 1,500-2,000 target. Trim if the adaptation pushes it over 2,500. Use imperative/infinitive form.
   - **Progressive disclosure**: Ensure SKILL.md has the workflow and cross-references to `references/` for detailed patterns.
5. **Validate** -- Run the same validation checklist as T2.
6. **Iterate** -- Fix any issues.

**Validation checklist (same as T2, adapted):**
- [ ] SKILL.md has valid YAML frontmatter
- [ ] `name` and `description` fields present
- [ ] Description uses third person with specific trigger phrases
- [ ] Body 1,500-2,000 words (hard max 2,500)
- [ ] Writing uses imperative/infinitive form
- [ ] Progressive disclosure implemented (workflow in SKILL.md, patterns in references/)
- [ ] All file references (`references/discovery-patterns.md`, `references/logql-syntax.md`) actually exist
- [ ] Examples are complete and working

**Procedure:**

1. Read `.claude/skills/discover-datasources/SKILL.md` (confirmed mostly clean of Bug 1-4 except Bug 2/3 in references).
2. Adapt SKILL.md:
   - Update YAML frontmatter: set `name` to "explore-datasources", write keyword-rich `description` following plugin-dev:skill-development third-person trigger-phrase style.
   - Preserve all four steps, all four examples, troubleshooting entries, Advanced Usage section, and Output Formats section.
   - Add cross-reference to `setup-gcx` skill (e.g., "If gcx is not configured, see the setup-gcx skill first").
   - Verify no `gcx graph` pipe references exist (none found in source).
   - Verify no `--all-versions` references exist (none found in source).
   - Verify body word count is within plugin-dev target (1,500-2,000 words).
3. Copy `references/discovery-patterns.md` with targeted fixes:
   - **Bug 2 fix**: Replace "Visualizing Query Results" section -- remove `| gcx graph` pipe examples, replace with `-o graph` codec examples.
   - **Bug 3 fix**: In "Saving Datasource UIDs" section, fix `jq -r '.[] | select(.type=="prometheus") | .uid'` to `jq -r '.datasources[] | select(.type=="prometheus") | .uid'` (and same for loki).
   - Verify no `--all-versions` references.
4. Copy `references/logql-syntax.md` as-is (verified: zero Bug 1-4 content).
5. Run final grep across all three output files for Bug 1-4 patterns.
6. Run the validation checklist above.

**Deliverables:**
- `claude-plugin/skills/explore-datasources/SKILL.md`
- `claude-plugin/skills/explore-datasources/references/discovery-patterns.md`
- `claude-plugin/skills/explore-datasources/references/logql-syntax.md`

**Acceptance criteria:**
- GIVEN `claude-plugin/skills/explore-datasources/SKILL.md` WHEN I compare it to `.claude/skills/discover-datasources/SKILL.md` THEN all four steps, all four examples, troubleshooting entries, and Advanced Usage section are present
- GIVEN the SKILL.md frontmatter WHEN I read it THEN `name` and `description` are present; description mentions datasource discovery, metrics, labels, log streams, datasource UIDs
- GIVEN the SKILL.md description WHEN I evaluate it against plugin-dev:skill-development standards THEN it uses third person, contains specific trigger phrases, and is between 1-3 sentences
- GIVEN the SKILL.md body WHEN I count words THEN it is between 1,500 and 2,500 words (target 1,500-2,000)
- GIVEN the SKILL.md content WHEN I search for `setup-gcx` THEN at least one cross-reference is found
- GIVEN `claude-plugin/skills/explore-datasources/references/discovery-patterns.md` WHEN I search for `| gcx graph` THEN zero matches are found
- GIVEN `claude-plugin/skills/explore-datasources/references/discovery-patterns.md` WHEN I search for `jq -r '.\[\]` (bare array access on datasource list) THEN zero matches are found; all datasource list jq examples use `.datasources[]`
- GIVEN `claude-plugin/skills/explore-datasources/references/logql-syntax.md` WHEN I diff against the source THEN files are identical (no modifications needed)
- GIVEN all three output files WHEN I search for `--all-versions` THEN zero matches are found

---

### T4b: Adapt investigate-alert skill
**Priority**: P0
**Effort**: Small
**Depends on**: T1
**Type**: task
**FRs**: FR-031, FR-032, FR-033, FR-034

Adapt `skills/investigate-alert/SKILL.md` from the existing `.claude/skills/grafana-investigate-alert/SKILL.md`. The source is clean (~135 lines, zero Bug 1-4 patterns), so adaptation is minimal: update frontmatter per `plugin-dev:skill-development` conventions, add a cross-reference to `setup-gcx`, and preserve the 4-step workflow.

**Plugin-dev:skill-development guidance for adaptation:**

Since the source is clean and self-contained, the 6-step process maps as follows:
1. **Understand** -- The skill teaches agents to investigate firing Grafana alerts. Target user intents: "why is this alert firing", "investigate alert", "what does this alert mean", "alert scope and impact".
2. **Plan** -- No reference files needed. The skill is self-contained (workflow + output format + error handling all in one file).
3. **Create structure** -- Directory created in T1 (update T1 deliverables to include `claude-plugin/skills/investigate-alert/`).
4. **Edit** -- Adapt SKILL.md with these plugin-dev standards:
   - **YAML frontmatter**: `name` ("investigate-alert") + `description` (third-person, trigger phrases: "Investigate Grafana alerts to determine why they are firing, their scope, and impact. Use when the user asks about a specific alert, wants to understand alert behavior, or needs to diagnose why an alert is in a firing or pending state.")
   - **Body**: Preserve the 4-step workflow verbatim (Verify Context, Get Alert Details with early exit, Full Investigation, Surface Resources). Verify word count is reasonable (source is ~135 lines; may be under 1,500-word target -- acceptable for a focused investigation skill).
   - **Cross-reference**: Add prerequisite note referencing setup-gcx skill (e.g., "If gcx is not configured, use the setup-gcx skill first").
5. **Validate** -- Run validation checklist (see below).
6. **Iterate** -- Fix any issues.

**Validation checklist:**
- [ ] SKILL.md has valid YAML frontmatter (opening and closing `---`)
- [ ] `name` and `description` fields present in frontmatter
- [ ] Description uses third person with specific trigger phrases
- [ ] Writing uses imperative/infinitive form
- [ ] 4-step workflow preserved (Verify Context, Get Alert Details, Full Investigation, Surface Resources)
- [ ] Early exit logic preserved (recording rules, healthy inactive alerts)
- [ ] Cross-reference to setup-gcx present
- [ ] Zero Bug 1-4 patterns (verify: no `auth.type`/`auth.token`, no `| gcx graph`, no bare `.[]` jq, no `--all-versions`)
- [ ] All gcx commands use correct syntax (`gcx query -d <uid> -e '<query>' -o json/graph`)

**Procedure:**

1. Read `.claude/skills/grafana-investigate-alert/SKILL.md` (confirmed clean: correct gcx commands, no Bug 1-4 patterns).
2. Adapt SKILL.md:
   - Update YAML frontmatter: set `name` to "investigate-alert", write keyword-rich `description` following plugin-dev:skill-development third-person trigger-phrase style. Remove `allowed-tools` field (per plan design decision: omit until semantics verified).
   - Preserve the entire 4-step investigation workflow, output format templates, error handling section, and tips section.
   - Add cross-reference to `setup-gcx` skill in the Prerequisites section (e.g., "If gcx is not configured, use the setup-gcx skill first").
   - Verify no Bug 1-4 patterns exist (none expected; source is clean).
3. Run final grep on output file for Bug 1-4 patterns.
4. Run the validation checklist above.

**Deliverables:**
- `claude-plugin/skills/investigate-alert/SKILL.md`

**Acceptance criteria:**
- GIVEN `claude-plugin/skills/investigate-alert/SKILL.md` WHEN I compare it to `.claude/skills/grafana-investigate-alert/SKILL.md` THEN all four investigation steps, output format templates, error handling, and tips are preserved
- GIVEN the SKILL.md frontmatter WHEN I read it THEN `name` is "investigate-alert" and `description` mentions alert investigation, firing alerts, scope, impact, and diagnosis
- GIVEN the SKILL.md description WHEN I evaluate it against plugin-dev:skill-development standards THEN it uses third person and contains specific trigger phrases
- GIVEN the SKILL.md content WHEN I search for `setup-gcx` THEN at least one cross-reference is found
- GIVEN the SKILL.md content WHEN I verify the 4-step workflow THEN Step 1 (Verify Context), Step 2 (Get Alert Details with early exit for recording rules and inactive alerts), Step 3 (Full Investigation with `-o json` and `-o graph`), and Step 4 (Surface Resources with analysis) are all present
- GIVEN the SKILL.md content WHEN I search for Bug 1-4 patterns (`auth.type`, `| gcx graph`, bare `.[]` on datasource list, `--all-versions`) THEN zero matches are found
- GIVEN the SKILL.md content WHEN I search for `gcx query` THEN all query examples use `-d <datasource-uid>` and `-o json` or `-o graph` (correct syntax)

---

## Wave 3: Agent Stub

### T5: Write grafana-debugger agent stub
**Priority**: P1
**Effort**: Small
**Depends on**: T1
**Type**: task
**FRs**: FR-019, FR-020, FR-021, FR-022, FR-029

Write `agents/grafana-debugger.md` as a system prompt stub. Follow `plugin-dev:agent-development` guidance for frontmatter structure, description with `<example>` blocks, and system prompt design. The agent CAN reference the `investigate-alert` skill by name since it ships in the same plugin.

**Plugin-dev:agent-development guidance:**

The agent file has two sections: YAML frontmatter and a system prompt body.

- **Frontmatter fields:**
  - `name`: kebab-case, 3-50 chars. Value: `grafana-debugger`
  - `description`: The MOST CRITICAL field for triggering. Must include 2-4 `<example>` blocks showing specific user utterances that should invoke this agent. Format:
    ```
    description: |
      Specialist agent for diagnosing application issues using Grafana
      observability data. Invoke when the user reports errors, latency
      problems, or service degradation and wants to investigate using
      metrics, logs, and SLOs.
      <example>My API is returning 500 errors, help me debug using Grafana</example>
      <example>Latency has spiked on the checkout service, investigate with Prometheus</example>
      <example>Find the root cause of this alert using Grafana metrics and logs</example>
    ```
  - `model`: omit (inherits from parent) or set to `inherit`
  - `color`: choose an appropriate color (e.g., `orange` for diagnostics/alerting)
  - `tools`: array of tools the agent needs. For gcx: `["Bash", "Read", "Grep"]` (Bash for running gcx commands, Read for reading output files, Grep for searching log content).

- **System prompt body:**
  - Written in second person ("You are a Grafana debugging specialist...")
  - Under 10,000 characters
  - Must define: responsibilities, process/methodology, output format expectations
  - MAY reference `investigate-alert` skill by name (ships in same plugin; useful for alert-related queries)

**Procedure:**

1. Write `agents/grafana-debugger.md` with YAML frontmatter:
   - `name`: "grafana-debugger"
   - `description`: Include 2-4 `<example>` blocks showing trigger conditions (error reports, latency spikes, service degradation, alert investigation).
   - `color`: "orange"
   - `tools`: `["Bash", "Read", "Grep"]`
2. Write system prompt body establishing diagnostic approach:
   - Discover datasources
   - Confirm scraping / data availability
   - Query error rates and latency percentiles
   - Correlate with log patterns from Loki
   - Summarize findings with evidence
3. Include note: always use `-o json` for machine-parseable output.
4. Include note: always use datasource UIDs, not names.
5. Include note: if gcx not configured, guide user through setup first.
6. The agent MAY reference `investigate-alert` by name (it ships in the same plugin). For alert-related queries, the system prompt can direct the agent to use the investigate-alert skill. Do NOT reference Stage 2 skills that do not yet exist.
7. Verify system prompt is under 10,000 characters.

**Deliverables:**
- `claude-plugin/agents/grafana-debugger.md`

**Acceptance criteria:**
- GIVEN `claude-plugin/agents/grafana-debugger.md` WHEN I read the frontmatter THEN `name` is "grafana-debugger" and `description` contains 2-4 `<example>` blocks showing trigger conditions
- GIVEN the frontmatter WHEN I check for `color` THEN it is present (e.g., "orange")
- GIVEN the frontmatter WHEN I check for `tools` THEN it is an array containing at minimum "Bash"
- GIVEN the agent body WHEN I count characters THEN it is under 10,000 characters
- GIVEN the agent body WHEN I read it THEN it is written in second person ("You are...")
- GIVEN the agent body WHEN I read it THEN the diagnostic approach is described (discover datasources, confirm scraping, query error rates, correlate logs, summarize findings)
- GIVEN the agent body WHEN I search for `-o json` THEN at least one mention is found
- GIVEN the agent body WHEN I search for "UID" THEN at least one mention is found about using datasource UIDs not names
- GIVEN the agent body WHEN I search for "setup" or "configured" THEN at least one mention is found about guiding users through setup if gcx is not configured

---

## Wave 4: Quality Gates

### T6: Validate plugin with plugin-dev meta-skills and verify all bugs fixed
**Priority**: P0
**Effort**: Medium
**Depends on**: T2, T3, T4, T4b, T5
**Type**: chore
**FRs**: FR-004, FR-005, FR-006, FR-007, FR-023, FR-024, FR-025, FR-026, FR-028, FR-030, FR-035

Run all quality gate validations using the specific processes defined by plugin-dev meta-skill agents. Fix any issues found during validation.

**Plugin-dev:skill-reviewer 8-step process (run once per skill):**

For each skill (`setup-gcx`, `explore-datasources`, `investigate-alert`):
1. Locate and read skill SKILL.md
2. Validate structure -- frontmatter format (opening/closing `---`), required fields (`name`, `description`)
3. Evaluate description -- trigger phrases present? Third person? Specific enough to differentiate from other skills? Length appropriate (1-3 sentences)?
4. Assess content quality -- word count (target 1,500-2,000, hard max 2,500), writing style (imperative/infinitive), logical organization (sections flow naturally)?
5. Check progressive disclosure -- is SKILL.md lean (workflow guidance) while references/ holds detailed data?
6. Review supporting files -- do all referenced files exist? Are reference files factually correct?
7. Identify issues by severity -- critical (blocks loading), major (degrades quality), minor (style nits)
8. Generate specific recommendations for each issue

**Plugin-dev:plugin-validator 10-step process:**

1. Locate plugin root (`claude-plugin/`), verify `.claude-plugin/plugin.json` exists
2. Validate manifest: JSON syntax valid, `name` field present, naming follows kebab-case
3. Validate directory structure: expected directories present (`agents/`, `skills/`)
4. Validate commands: N/A (no commands in this plugin)
5. Validate agents: frontmatter format, required fields (`name`, `description`), `color` present, system prompt present
6. Validate skills: SKILL.md exists in each skill directory, frontmatter valid, all references/ files exist
7. Validate hooks: N/A (no hooks in this plugin)
8. Validate MCP: N/A (no MCP server in this plugin)
9. Check file organization: no temp files, no stale artifacts
10. Security checks: no hardcoded credentials, tokens, or secrets in any file

**Procedure:**

1. **Bug verification (all 4 bugs across all plugin files):**
   - Grep all files under `claude-plugin/` for `auth.type`, `auth.token`, `auth.username`, `contexts.*.namespace` as config targets -- expect zero matches (Bug 1).
   - Grep for `| gcx graph` and `gcx graph` as standalone -- expect zero matches (Bug 2).
   - Grep for `'.[0].uid'` and `'.[]` on datasource list output -- expect zero matches (Bug 3).
   - Grep for `--all-versions` -- expect zero matches (Bug 4).

   **Negative constraint verification (spec lines 247-256):**
   - Grep for `gcx resources edit` -- expect zero matches.
   - Grep for `GCX_AGENT_MODE` -- expect zero matches.
   - Grep for `--max-concurrent` or `errgroup` -- expect zero matches.
   - Grep for `selector resolution` or `parse.*discover.*match.*resolve` -- expect zero matches.
   - Verify no `mcpServers`, `commands/`, or `hooks` directories exist in the plugin.

2. **Skill reviews (plugin-dev:skill-reviewer 8-step process):**
   - Run the 8-step process on `skills/setup-gcx/SKILL.md`. Address any major or critical issues.
   - Run the 8-step process on `skills/explore-datasources/SKILL.md`. Address any major or critical issues.
   - Run the 8-step process on `skills/investigate-alert/SKILL.md`. Address any major or critical issues.
   - For each skill, verify: description trigger phrases are specific and non-overlapping, word count within target, progressive disclosure implemented where applicable, all referenced files exist.

3. **Plugin validation (plugin-dev:plugin-validator 10-step process):**
   - Run the 10-step process on `claude-plugin/`. Address any critical issues.
   - Specifically verify: manifest JSON is valid, agent frontmatter includes `<example>` blocks, all skill reference files exist, no security issues.

4. **Plugin loading test:**
   - Run `claude --plugin-dir ./claude-plugin` and verify it loads without errors.
   - Verify all three skills appear in the skill list (setup-gcx, explore-datasources, investigate-alert).
   - Test auto-triggering: "help me set up gcx" should trigger setup-gcx.
   - Test auto-triggering: "what datasources does my Grafana have" should trigger explore-datasources.
   - Test auto-triggering: "why is this alert firing" should trigger investigate-alert.

5. **Fix any issues** found during validation. Re-run the relevant validation step after fixes until all gates pass.

**Deliverables:**
- All plugin files passing validation (no new files; this task modifies existing files from T1-T5 if issues are found)
- Verification log confirming all 4 bug patterns have zero matches

**Acceptance criteria:**
- GIVEN all plugin content files WHEN I grep for Bug 1-4 patterns THEN zero matches across all four bug categories
- GIVEN `skills/setup-gcx/SKILL.md` WHEN reviewed by the skill-reviewer 8-step process THEN no major or critical issues reported
- GIVEN `skills/explore-datasources/SKILL.md` WHEN reviewed by the skill-reviewer 8-step process THEN no major or critical issues reported
- GIVEN `skills/investigate-alert/SKILL.md` WHEN reviewed by the skill-reviewer 8-step process THEN no major or critical issues reported
- GIVEN the complete plugin WHEN validated by the plugin-validator 10-step process THEN passes without critical errors; plugin contains 3 skills and 1 agent
- GIVEN the plugin at repo root WHEN loaded with `claude --plugin-dir ./claude-plugin` THEN plugin loads without errors and all three skills appear in the skill list
- GIVEN the plugin is loaded WHEN user says "help me set up gcx" THEN setup-gcx skill triggers
- GIVEN the plugin is loaded WHEN user says "what datasources does my Grafana have" THEN explore-datasources skill triggers
- GIVEN the plugin is loaded WHEN user says "why is this alert firing" THEN investigate-alert skill triggers
