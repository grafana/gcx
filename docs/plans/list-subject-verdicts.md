# `list-<subject>` family — adjudication verdicts (#387)

Status record for the addressability rule in the Command Operation Semantics ADR §8
(`docs/adrs/command-operation-contract/001-command-operation-semantics.md` on
`awsome-o/387-command-operation-contract-adr`, PR #994): *things with their own ID get a
noun group (`things list` / `things get <id>`); `list-things` is reserved for (a) plain
value enumerations with no ID of their own and (b) sub-lists addressed by the parent's ID.*

This answers the maintainer's question on that rule ("let's see how many commands we'd
have to change", PR #994 thread r3616693985): **28 commands across 12 providers; every
adjudicated record resolved to rename, none to keep.** Two were executed first as the D4c
pilot (PR #1009); wave 1 executed 12 (PRs #1013, #1014); the remaining 14 land in the
wave-2 provider batches. Each verdict below was made by an independent judge with code
citations and survived an adversarial verification pass (2026-07-20 session); verdicts are
recorded verbatim.

## Pilot (PR #1009, decided D4c)

| Current | Target | Basis |
|---|---|---|
| `gcx profiles profile-types` | `gcx profiles list-types` | §8 case (a): discovery/catalog facet scoped by `--datasource`; profile types have no ID-addressed read-one |
| `gcx datasources pyroscope profile-types` | `gcx datasources pyroscope list-types` | Same shared builder at its second mount |

## Executed in wave 1

### agento11y — #1013 (wave 1)

| Current | Verdict | Target |
|---|---|---|
| `gcx agento11y agents versions` | rename | `gcx agento11y agents list-versions` |
| `gcx agento11y collections conversations list` | rename | `gcx agento11y collections list-conversations <collection-id>` |
| `gcx agento11y experiments scores` | rename | `gcx agento11y experiments list-scores` |
| `gcx agento11y judge models` | rename | `gcx agento11y judge list-models` |
| `gcx agento11y judge providers` | rename | `gcx agento11y judge list-providers` |
| `gcx agento11y scores list` | rename | `gcx agento11y generations list-scores <generation-id>` |
| `gcx agento11y templates versions` | rename | `gcx agento11y templates list-versions` |

- **`gcx agento11y agents versions`** — ADR §8 clause 2: version history is a parent-scoped collection addressed by the agent's name with no standalone version identity (only read-one is `agents get --version`, a flag qualifier), so the compound `list-versions <agent-name>` is the approved spelling — matching the live `alert-groups list-alerts <group-id>` exemplar and the parallel templates/dashboards versions dispositions.
- **`gcx agento11y collections conversations list`** — ADR §8 case (b): the first required positional is the parent collection's ID, so the parent-scoped sub-list takes the $PARENT $OPERATION-$CHILD compound (alert-groups list-alerts precedent); the current nested noun group is in the ADR's explicitly-rejected list.
- **`gcx agento11y experiments scores`** — ADR §8 clause 2: parent-scoped collection addressed by the run's (parent's) ID with no score read-one anywhere in the surface — §8's own worked example prescribes the compound `experiments list-scores <run-id>`.
- **`gcx agento11y judge models`** — ADR §8 list-<subject> clause 1: judge models are a flag-scoped (--provider) discovery/catalog facet with no fetch-one-by-ID surface anywhere, mirroring the ratified cloudwatch list-metrics family, so the compound spelling of list applies.
- **`gcx agento11y judge providers`** — ADR §8 list-subject clause 1: judge providers are a plugin-scoped capability catalog with no own-ID read-one anywhere (IDs appear only as filter/config values), so the compound spelling of list is canonical per the ratified cloudwatch list-* analog, and the bare noun leaf is neither an approved §3 operation nor a §4 shorthand.
- **`gcx agento11y scores list`** — ADR §8 case (b): the positional is the parent generation's ID and scores have no fetch-one surface anywhere (CLI, client, typed adapters), so the parent-scoped compound under the addressable parent applies — the exact shape of §8's experiments list-scores worked example and alert-groups list-alerts.
- **`gcx agento11y templates versions`** — ADR §8 list-<subject> rule case 2: a parent-scoped collection addressed solely by the parent template's ID, with no version read-one anywhere in the CLI, takes the compound spelling `list-versions <template-id>` (same shape as ratified `alert-groups list-alerts <group-id>`).

### datasources — #1014 (wave 1)

| Current | Verdict | Target |
|---|---|---|
| `gcx datasources athena describe-table` | rename | `gcx datasources athena list-columns` |
| `gcx datasources influxdb field-keys` | rename | `gcx datasources influxdb list-field-keys` |
| `gcx datasources influxdb measurements` | rename | `gcx datasources influxdb list-measurements` |
| `gcx datasources influxdb tag-keys` | rename | `gcx datasources influxdb list-tag-keys` |
| `gcx datasources influxdb tag-values` | rename | `gcx datasources influxdb list-tag-values` |

- **`gcx datasources athena describe-table`** — Fails §5's materially-different-output test for describe (plain []string of names, nothing schema-like) and §8 conditions 1+2 approve the compound: columns are a non-addressable parent-scoped facet reached via the table's identity, matching the ratified athena list-* family and the rollout plan's explicit Athena ≈ list-columns hint.
- **`gcx datasources influxdb field-keys`** — ADR §8 condition 1 (via §4's closed shorthand set excluding field-keys): a datasource-scoped value enumeration with no ID of its own and no get anywhere takes the list-&lt;subject&gt; compound, exactly parallel to the ratified clickhouse/cloudwatch/athena list-* family and D4c profile-types→list-types.
- **`gcx datasources influxdb measurements`** — ADR §8 list-&lt;subject&gt; condition 1: measurements are a --datasource-scoped discovery/catalog facet with no ID of their own (no read-one; --measurement on siblings is a filter flag) and outside the closed §4 shorthand set, so the compound spelling of list applies, parallel to ratified cloudwatch/athena/clickhouse list-* and decided D4c profile-types→list-types.
- **`gcx datasources influxdb tag-keys`** — Tag keys are ID-less, datasource-scoped discovery values (SHOW TAG KEYS → []string; no read-one exists) → ADR §8 list-&lt;subject&gt; condition 1, exactly parallel to the ratified cloudwatch/athena/clickhouse list-* family and the decided profile-types→list-types rename (§4/D4c); tag-keys is outside the closed §4 shorthand set.
- **`gcx datasources influxdb tag-values`** — Tag values are not independently addressable (no ID, no get anywhere; --key is the parent tag key's flag-supplied identity), so ADR §8's list-&lt;subject&gt; compound applies — same shape as the ratified cloudwatch/athena/clickhouse list-* family and D4c list-types.

## Wave 2 (this PR series)

### assistant — assistant batch (wave 2)

| Current | Verdict | Target |
|---|---|---|
| `gcx assistant investigations approvals` | rename | `gcx assistant investigations list-approvals` |
| `gcx assistant investigations todos` | rename | `gcx assistant investigations list-todos` |
| `gcx assistant investigations tools` | rename | `gcx assistant investigations list-tool-calls` |

- **`gcx assistant investigations approvals`** — ADR §8 case 2 (parent-scoped collection, positional is the investigation's ID, no fetch-one by approval ID anywhere) → compound spelling list-approvals, same shape as ratified alert-groups list-alerts.
- **`gcx assistant investigations todos`** — ADR §8 case (b): parent-scoped collection addressed by the investigation's ID with no independently addressable todo (no todo/agent read-one exists), so the compound `list-todos` is the approved spelling — same shape as the ratified `alert-groups list-alerts <group-id>`; flag the v2-consolidation removal option to the assistant owner per §9 before executing.
- **`gcx assistant investigations tools`** — ADR §8 case (b): tool calls are a parent-scoped collection addressed by the investigation's ID with no read-one anywhere, so the compound spelling of list — list-tool-calls — is required, matching the alert-groups list-alerts precedent.

### irm — irm batch (wave 2)

| Current | Verdict | Target |
|---|---|---|
| `gcx irm incidents activity list` | rename | `gcx irm incidents list-activity` |
| `gcx irm incidents contexts list` | rename | `gcx irm incidents list-contexts` |
| `gcx irm oncall schedules final-shifts` | rename | `gcx irm oncall schedules list-final-shifts` |

- **`gcx irm incidents activity list`** — ADR §8 case (b): the sole required positional is the parent incident's ID and no gcx command or client method addresses an ActivityItemID on its own, so the parent-scoped collection takes the compound $PARENT list-$CHILD $PARENT_ID, matching the ratified alert-groups list-alerts <group-id>.
- **`gcx irm incidents contexts list`** — ADR §8 branch (b): the required positional is the parent incident's ID and contexts are not independently addressable anywhere in the exposed surface (no read-one, no ContextID filter), so the parent-scoped compound `incidents list-contexts <incident-id>` applies — matching the ratified alert-groups list-alerts <group-id> exemplar; the catalog-children carve-out (severities, no positional) does not apply.
- **`gcx irm oncall schedules final-shifts`** — ADR §8 condition 2: a parent-scoped collection addressed by the parent schedule's ID (ExactArgs(1) → ListFilterEvents, oncall_commands_extra.go:1105) whose members have no ID of their own takes the compound spelling list-final-shifts, matching the ratified alert-groups list-alerts <group-id> shape.

### k6 — k6 batch (wave 2)

| Current | Verdict | Target |
|---|---|---|
| `gcx k6 load-zones allowed-load-zones list` | rename | `gcx k6 projects list-allowed-load-zones` |
| `gcx k6 load-zones allowed-projects list` | rename | `gcx k6 load-zones list-allowed-projects` |

- **`gcx k6 load-zones allowed-load-zones list`** — ADR §8 case (b): the positional is the parent project's ID and allowed-load-zones is a non-addressable project-scoped membership set (api.go:60-61, no read-one), so the compound list-allowed-load-zones belongs under projects — the identity owner — per the precise statement ($PARENT $OPERATION-$CHILD $PARENT_ID) and the explicit rejection of nested noun groups.
- **`gcx k6 load-zones allowed-projects list`** — ADR §8 case (b): the required positional is the parent load zone's ID and the membership set has no ID/get of its own (api.go:58-61), so the parent-scoped compound `load-zones list-allowed-projects <load-zone-id>` applies (the alert-groups list-alerts shape); nested noun groups are explicitly rejected.

### appo11y — tail sweep (wave 2)

| Current | Verdict | Target |
|---|---|---|
| `gcx appo11y services labels` | rename | `gcx appo11y services list-labels` |

- **`gcx appo11y services labels`** — ADR §8 case (b): the positional is the parent service's ID and labels are a parent-scoped, non-independently-addressable collection (--label is a narrowing filter returning a one-element Items list), while §4/§10 confine the `labels` shorthand to signal/per-datasource families excluding appo11y — so the compound list-labels, matching sibling `services list-operations <service>` which §8 cites verbatim.

### cloud — tail sweep (wave 2)

| Current | Verdict | Target |
|---|---|---|
| `gcx cloud stacks regions` | rename | `gcx cloud stacks list-regions` |

- **`gcx cloud stacks regions`** — ADR §8 case (a): regions are a plain value enumeration (GET /api/stack-regions → []Region collection, no read-one anywhere on the CLI, consumed only as stacks create --region values) — the exact analog of the cloudwatch list-regions exemplar §8 names, and a bare noun leaf hides the list operation (§2 rubric).

### dashboards — tail sweep (wave 2)

| Current | Verdict | Target |
|---|---|---|
| `gcx dashboards versions list` | rename | `gcx dashboards list-versions` |

- **`gcx dashboards versions list`** — ADR §8 case (b): the positional is the parent dashboard's name and a version has no ID of its own (no get anywhere; items share the parent's metadata.name, distinguished only by generation), so the parent-scoped compound `dashboards list-versions <name>` is required — consistent with alert-groups list-alerts, the CONSTITUTION $PARENT $VERB-$CHILD $PARENT_ID clause, and this wave's agento11y list-versions companions.

### dev — tail sweep (wave 2)

| Current | Verdict | Target |
|---|---|---|
| `gcx dev lint rules` | rename | `gcx dev lint list-rules` |

- **`gcx dev lint rules`** — ADR §8 case (1): linter rules are a discovery/catalog facet with no fetch-one by rule name anywhere in CLI or engine (rule names are only --disable/--enable filter values), so the list-<subject> compound applies — same shape as decided D4c profile-types→list-types.

### frontend — tail sweep (wave 2)

| Current | Verdict | Target |
|---|---|---|
| `gcx frontend apps show-sourcemaps` | rename | `gcx frontend apps list-sourcemaps` |

- **`gcx frontend apps show-sourcemaps`** — ADR §3 (returns a []SourcemapBundle collection → op is list), §5 (show not canonical), §8 case (b) (parent app-ID positional, bundles not independently addressable — no read-one, delete routes through parent path) → compound list-sourcemaps, consistent with alert-groups list-alerts and the ratified list-* family.

### resources — tail sweep (wave 2)

| Current | Verdict | Target |
|---|---|---|
| `gcx resources examples` | rename | `gcx resources list-examples` |

- **`gcx resources examples`** — ADR §8 situation 1: example manifests are a discovery/catalog facet with no ID of their own (keyed by the resource type's GVK; the optional selector is a parent-type filter, and no example read-one exists), so the compound spelling of `list` applies — consistent with D4c profile-types→list-types and the ratified list-* family.

## Non-members, for the record

`gcx irm incidents severities list` keeps its noun group under the ADR §8 catalog-children
carve-out (no parent identity in the addressing path). Wave-1 keeps adjacent to this family:
`agento11y saved-conversations collections` (collections are independently addressable;
panel split) and the pyroscope `exemplars profile`/`span` pair (shared builder mounted under
two trees; convergence deferred to the profiles batch). See the wave-1 PR bodies (#1013,
#1014) for those dispositions.
