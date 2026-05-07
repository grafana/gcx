# SM dev-cluster coverage gaps

**Date:** 2026-05-06
**Author:** mark.poko@grafana.com
**Stack analyzed:** `dev.grafana-dev.net` (gcx context `mpoko-gcx-test-dev`)
**SM datasource UID:** `fexk3zyyowkjkd`
**Snapshot:** 64 SM checks, 58 distinct targets

## Summary

Diffing live Synthetic Monitoring checks on Grafana's dev stack against the
Terraform-generated cluster registries in `deployment_tools/` surfaces **38+
unmonitored dev-cluster endpoints across four product pillars**:

| Pillar | Cells in registry | Cells monitored | Coverage |
|---|---:|---:|---:|
| Mimir / Cortex | 12 | 11 | 92% |
| Hosted Grafana (fsdev login) | 7 | 5 | 71% |
| Admin pages | 12 | 12 | 100% |
| PDC API/SSH | 2 (per `pdc.json`) | 2 + 2 extras | 100%¹ |
| **Loki** | **13** | **0** | **0%** |
| **Tempo** | **7** | **0** | **0%** |
| **Pyroscope / Phlare** | **5** | **0** | **0%** |
| **Alertmanager** | **5** | **0** | **0%** |

¹ SM additionally probes PDC on `dev-us-east-0` and `dev-us-east-3` — neither
appears in `pdc.json`. Likely stale; flagged for audit.

**Headline finding:** the dev stack has **zero external monitoring** for
Loki, Tempo, Pyroscope, and Alertmanager. Mimir and Hosted Grafana are
near-complete; the missing cells are listed below.

## Methodology

This analysis became practical thanks to four new server-side filter
parameters added to `GET /api/v1/check` in
[synthetic-monitoring-api PR #2110](https://github.com/grafana/synthetic-monitoring-api/pull/2110)
(merged 2026-05-05): `search`, `enabled`, `min_frequency`, `max_frequency`.
gcx exposes them as `--search`, `--enabled`, `--min-frequency`,
`--max-frequency` on `gcx datasources synthetic-monitoring checks`.

The diff itself is straightforward:

1. **Live checks** — pull all SM checks on dev:
   ```bash
   gcx --context mpoko-gcx-test-dev datasources synthetic-monitoring checks \
     -d fexk3zyyowkjkd \
     | jq -r '.[].target' | sort -u
   ```
2. **Source of truth** — read cluster registries in
   `deployment_tools/ksonnet/lib/meta/raw/clusters/generated/*.json` and
   per-product cell directories under
   `deployment_tools/ksonnet/environments/{cortex,loki,tempo,phlare,alertmanager,...}/dev-*.{cell}/`.
3. **Compare** — diff registry-derived hostnames against the live targets.

## The JSON registries

| File | Purpose | Key field used |
|---|---|---|
| `ksonnet/lib/meta/raw/clusters/generated/base.json` | All clusters with `status` | `clusters[].status == "dev"` |
| `ksonnet/lib/meta/raw/clusters/generated/hg.json` | Hosted-grafana gateway IPs (drives `fsdev*.grafana-dev.net`) | cluster keys |
| `ksonnet/lib/meta/raw/clusters/generated/pdc.json` | PDC public IPs (drives `private-datasource-connect-*`) | cluster keys |
| `ksonnet/lib/meta/raw/clusters/generated/components.json` | Per-cluster component install map | cluster keys |

Per-product cells live in `ksonnet/environments/{product}/dev-{region}.{cell}/`.

The `fsdev` hostname pattern is generated programmatically by
`ksonnet/environments/betterops-grafana-frontend/sm.libsonnet:22`:
```jsonnet
local host = 'fs%s.%s' % [std.strReplace(cluster, '-', ''), domain];
// dev-us-east-3 → fsdevuseast3.grafana-dev.net
```

## Missing endpoints

### 🟥 Loki — 0 of 13 dev cells monitored

```
dev-eu-west-2.loki-dev-009
dev-eu-west-4.loki-dev-015
dev-eu-west-6.loki-dev-018
dev-us-central-0.loki-dev-005
dev-us-central-0.loki-dev-005-gf
dev-us-central-0.loki-dev-006
dev-us-central-0.loki-dev-010
dev-us-central-0.loki-thor-demo            (likely internal/demo — confirm before probing)
dev-us-east-0.loki-dev-001
dev-us-east-0.loki-dev-002
dev-us-east-0.loki-dev-012
dev-us-east-0.loki-dev-017
dev-us-east-3.loki-dev-016
```

### 🟥 Tempo — 0 of 7 dev cells monitored

```
dev-eu-west-2.tempo-dev-04
dev-eu-west-6.tempo-dev-07
dev-eu-west-6.tempo-dev-08
dev-us-central-0.tempo-dev-01
dev-us-east-0.tempo-dev-02
dev-us-east-0.tempo-dev-test-03            (test cell — likely exclude)
dev-us-east-3.tempo-dev-06
```

### 🟥 Pyroscope / Phlare — 0 of 5 dev cells monitored

```
dev-eu-west-2.profiles-dev-005
dev-eu-west-6.profiles-dev-007
dev-us-east-0.profiles-dev-002
dev-us-east-3.profiles-dev-006
dev-us-central-0.fire-dev-001              (legacy/experimental — confirm before probing)
```

### 🟥 Alertmanager — 0 of 5 dev cells monitored

```
dev-eu-west-2.alertmanager
dev-eu-west-6.alertmanager
dev-us-central-0.alertmanager
dev-us-east-0.alertmanager
dev-us-east-3.alertmanager
```

### 🟧 Hosted Grafana (fsdev login probes) — 5 of 7

Missing per `hg.json`:

```
fsdeveuwest4.grafana-dev.net/login         (dev-eu-west-4)
fsdeveuwest5.grafana-dev.net/login         (dev-eu-west-5)
```

The five covered cells (`us-central-0`, `us-east-0`, `us-east-3`, `eu-west-2`,
`eu-west-6`) each get both browser and http variants. The two missing cells
should follow the same shape — and since
`betterops-grafana-frontend/sm.libsonnet` derives checks from
`metaEnvs.by_app('grafana-frontend-service')`, this is fixable by ensuring
those clusters are tagged with that app.

### 🟧 Mimir / Cortex — 11 of 12 cells monitored

Missing:

```
dev-us-central-0.mimir-dev-28              → prometheus-dev-28-dev-us-central-0.grafana-dev.net
```

### 🟩 Admin pages — 12 of 12 (full coverage)

Every ordinary dev cluster in `base.json` (i.e., `status: "dev"` excluding the
3 `pop-dev-*` regional PoPs and `capi-dev-eu-west-0` CAPI-mgmt cluster) has an
admin-page check.

### 🟩 PDC — 2 of 2 in registry, plus 2 extras to audit

Covered cleanly: `dev-eu-west-2`, `dev-us-central-0` (both `:443` API and
`:22` SSH).

**Audit candidates:** SM also probes PDC on
`private-datasource-connect-api-dev-us-east-0.grafana-dev.net:443` and
`private-datasource-connect-api.dev-us-east-3.grafana-dev.net:443`, but
neither cluster appears in `pdc.json`. These probes may be stale —
recommend confirming PDC is actually deployed there before keeping them
green-locked.

## Recommended priority order

| # | Action | Cells | Why |
|---|---|---:|---|
| 1 | Add Loki dev probes | 12 | Largest single gap; complete absence of an observability pillar |
| 2 | Add Tempo dev probes | 6 | Same as above for traces |
| 3 | Add Pyroscope dev probes | 4 | Same as above for profiles |
| 4 | Add Alertmanager dev probes | 5 | Ops-critical; failures mask escalation paths |
| 5 | Fill `fsdev` `eu-west-4` + `eu-west-5` | 2 | Identical to 5 existing checks; just metadata fix |
| 6 | Add `prometheus-dev-28` Mimir probe | 1 | Lone Mimir gap |
| 7 | Audit `us-east-0` / `us-east-3` PDC probes | (2 audit) | Probably stale per `pdc.json` |

## Reproduction commands

The new SM API filters make this whole exercise rerunnable in seconds.

```bash
# Pull live targets on dev
gcx --context mpoko-gcx-test-dev datasources synthetic-monitoring checks \
  -d fexk3zyyowkjkd | jq -r '.[].target' | sort -u

# Find disabled (stale) checks
gcx --context mpoko-gcx-test-dev datasources synthetic-monitoring checks \
  -d fexk3zyyowkjkd --enabled=false

# Find aggressive (≤1 min) checks — load and cost concerns
gcx --context mpoko-gcx-test-dev datasources synthetic-monitoring checks \
  -d fexk3zyyowkjkd --max-frequency 1m | jq 'length'

# Find lazy (≥5 min) checks — under-monitoring concerns
gcx --context mpoko-gcx-test-dev datasources synthetic-monitoring checks \
  -d fexk3zyyowkjkd --min-frequency 5m | jq 'length'

# Scope queries to a domain or cluster — server-side substring match on job + target
gcx --context mpoko-gcx-test-dev datasources synthetic-monitoring checks \
  -d fexk3zyyowkjkd --search prometheus-dev-28
```

Cluster registries:

```bash
cd ~/grafana/deployment_tools/ksonnet/lib/meta/raw/clusters/generated
jq '.clusters | to_entries | map(select(.value.status == "dev")) | .[].key' base.json
jq -r '.clusters | keys[]' hg.json | grep '^dev-'
jq -r '.clusters | keys[]' pdc.json | grep '^dev-'
```

Per-product dev cells:

```bash
cd ~/grafana/deployment_tools/ksonnet/environments
ls cortex/        | grep '^dev-'   # Mimir / Cortex
ls loki/          | grep '^dev-'   # Loki
ls tempo/         | grep '^dev-'   # Tempo
ls phlare/        | grep '^dev-'   # Pyroscope / Phlare
ls alertmanager/  | grep '^dev-'   # Alertmanager
```

## Caveats and confidence

- ~95% confident on cell enumeration — read directly from
  `ksonnet/environments/{product}/dev-*` directories.
- ~90% confident on cluster registry counts — `hg.json`, `pdc.json`,
  `base.json` are Terraform-generated with an explicit `__generated` marker.
- The user-facing hostname for each missing cell **should be deterministic by
  convention** (`<resource>-<cell-suffix>-<cluster>.grafana-dev.net`), but
  this analysis did not verify that DNS resolves for every specific
  Loki/Tempo/Pyroscope cell. Those products may use different hostname
  patterns (e.g., Loki cells use both `logs-*` and `loki-*` naming); the
  externally-resolvable form should be confirmed against
  `terraform/cells/{loki,tempo,phlare}/dev-*/dns.tf` before drafting check
  definitions.
- `loki-thor-demo`, `tempo-dev-test-03`, `fire-dev-001` are flagged as likely
  internal / legacy / experimental — confirm with the owning team before
  adding probes.
- This snapshot reflects 2026-05-06 state. Cell deployments and SM coverage
  both change; rerun the reproduction commands to refresh.

## Related work

- [synthetic-monitoring-api PR #2110](https://github.com/grafana/synthetic-monitoring-api/pull/2110)
  — added the filter parameters used here.
- gcx branch `pokom/sm-datasource` — wires those filters into
  `gcx datasources synthetic-monitoring checks`.
- Prior SM datasource research:
  [`2026-05-04-sm-datasource.md`](2026-05-04-sm-datasource.md),
  [`2026-05-04-sm-datasource-summary.md`](2026-05-04-sm-datasource-summary.md),
  [`2026-05-04-sm-datasource-oauth-gap.md`](2026-05-04-sm-datasource-oauth-gap.md).
