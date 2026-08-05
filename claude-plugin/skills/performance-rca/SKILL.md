---
name: performance-rca
description: >
  Profiling-led root cause analysis (RCA): locate WHERE a service spends its
  CPU, memory, or lock-wait time using Pyroscope profiles via gcx — down to
  the function and file:line — and in which slice of the fleet. Use when the
  question or the evidence points at the service's own code: "why is
  <service> slow", "CPU usage is high", "memory keeps growing", "find the
  hotspot / hot functions", "lock contention", "goroutine leak", "did the
  deploy make it slower", "which code path regressed", "analyze the profile /
  flame graph of <service>". This skill locates the cost; it does not write
  or propose code fixes. NOT for general incident triage across metrics,
  logs, and traces (use debug-with-grafana), firing alerts (use
  investigate-alert), or SLO breaches (use slo-investigate) — come here from
  those workflows once the evidence points at the service's own code.
---

# Performance RCA (profiling-led)

Locate where a service spends its resources: which service, which slice of
the fleet, which function, at which source line, and — when a regression —
since when. The deliverable is a **location report**, not a fix.

## Determinism rules

Follow these on every run; they are what make the outcome reproducible:

1. Run the steps in order. Never skip the admission gate (Step 1).
2. Never guess a profile type ID or a label name — both are
   deployment-specific. Always copy them from the discovery commands.
3. Every query below has a defined empty/error branch — follow it instead of
   improvising or retrying with varied parameters.
4. Always end with the report in Step 7, even when the answer is a bounded
   negative ("cost is not in this service's code because ...").
5. Do not add `-o json` to `profiles query` — it returns the raw flame-graph
   (renderer format, not analyzable). Leave the output format alone.

## Step 1: Admission gate

Three checks with hard exits. Do not proceed past a failing check.

```bash
# 1a. Is there a Pyroscope datasource at all?
gcx datasources list -t grafana-pyroscope-datasource -o json

# 1b. Does it hold data, and over what range?
gcx profiles data-range -d <pyro-uid> -o json

# 1c. Which profile types exist? (needed for every later step)
gcx profiles list-profile-types -d <pyro-uid> -o json
```

- **EXIT A — no Pyroscope datasource**: profiling is not connected to this
  stack. Say so, point at the `setup-gcx` skill and the Pyroscope
  instrumentation docs, and offer to continue the investigation without
  profiles via `debug-with-grafana`. Stop here.
- **EXIT B — `dataIngested: false`**: the tenant never received profiling
  data. Same exit as A.
- **EXIT C — the window of interest falls outside the reported bounds**
  (`data_ingested` is a lifetime flag; bounds are the currently queryable
  range — data ages out after the retention period, 31 days by default):
  state the queryable range, and either analyze the available window (warning
  that it describes the present, not the incident) or stop, at the user's
  choice.

## Step 2: Pick the lens (profile type)

Match the symptom to a profile type. Use the exact ID from Step 1c — the IDs
below are the common Go-runtime forms and may differ per runtime:

| Symptom | Lens (profile type) |
|---|---|
| Slow requests, high CPU, throttling | `process_cpu:cpu:nanoseconds:cpu:nanoseconds` |
| Memory growing / OOM / GC pressure | `memory:alloc_space:bytes:space:bytes` (allocation churn) and `memory:inuse_space:bytes:space:bytes` (what holds memory now) |
| Requests wait but CPU is idle; suspected lock contention | `mutex:delay:nanoseconds:contentions:count` and `block:delay:nanoseconds:contentions:count` |
| Goroutine/thread leak | `goroutines:goroutine:count:goroutine:count` |

If the symptom is ambiguous, start with CPU, then check alloc_space — the two
cover most cases and the funnel below is identical for every lens.

## Step 3: Establish the two windows

Comparison is the method: a finding is a *difference*, not a big number.

- **Incident window**: from the user, or from the triage that routed here
  (metrics onset time). If neither exists, use the last hour.
- **Baseline window**: same length, before the incident; prefer the same
  time-of-day (yesterday's identical window) over the immediately preceding
  one when traffic is diurnal.
- Both windows must fall inside the Step-1b bounds; shrink or shift per EXIT
  C rules if they don't.

## Step 4: Narrow — tenant → service → slice

```bash
# 4a. Which services burn this resource (top 10; deliberately tenant-wide).
#     If the tenant has hundreds of services, scope with '{namespace="..."}'
gcx profiles metrics -d <pyro-uid> '{}' \
  --profile-type <lens> --from <incident-from> --to <incident-to> --top -o json

# 4b. Which labels can slice the suspect (region, pod, version, ...)
gcx profiles series -d <pyro-uid> '{service_name="<suspect>"}' \
  --from <incident-from> --to <incident-to> -o json

# 4c. Is the cost uniform or localized? Rank by one label from 4b
gcx profiles metrics -d <pyro-uid> '{service_name="<suspect>"}' \
  --profile-type <lens> --from <incident-from> --to <incident-to> \
  --top --group-by <label> -o json
```

Empty/error branches:
- 4a empty → the resource is not being consumed in this window at all;
  re-check the lens (Step 2) once, then report the bounded negative (Step 7).
- 4a/4c entries with `total: 0` carry no signal (no samples in the window) —
  treat them as absent, never as a ranked suspect.
- 4c returns a single group → that label cannot slice this service; try one
  other label from 4b, else treat as uniform.
- 4c uniform (all values within ~2x of each other) → the cost is fleet-wide:
  analyze the whole service in Step 5, and say "uniform" in the report.
- 4c localized (one value dominates) → that label=value pair is the slice;
  it is itself a finding (localization conditions belong in the report).

## Step 5: Locate the function

Run the same query for the hottest slice in BOTH windows and compare:

```bash
# Incident window
gcx profiles query -d <pyro-uid> '{service_name="<suspect>", <label>="<hot-value>"}' \
  --profile-type <lens> --from <incident-from> --to <incident-to>

# Baseline window (same selector, same length)
gcx profiles query -d <pyro-uid> '{service_name="<suspect>", <label>="<hot-value>"}' \
  --profile-type <lens> --from <baseline-from> --to <baseline-to>
```

Reading rules (this is where wrong answers happen — apply all three):

1. **The culprit is the biggest difference between the windows** (or between
   the hot slice and a normal slice), never the biggest number in one output.
2. **Attribute to application frames by TOTAL, not runtime leaves by SELF.**
   Runtime/stdlib leaves describe *how* the resource burns, not *whose fault*:
   `runtime.nanotime` under `time.Since` = busy-wait loop;
   `runtime.mallocgc` = allocation pressure; `runtime.futex`/`runtime.lock` =
   lock contention; `gcBgMarkWorker`/`gcDrain` = GC pressure;
   `chanrecv`/`chansend` = channel blocking. The application function above
   them whose TOTAL contains their time is the location to report.
3. **A clean table with no dominant difference is a finding, not a failure**:
   the cost is off-CPU (switch lens to mutex/block, once) or outside this
   service (report the bounded negative and route back to debug-with-grafana).

When the table's top-20 view or 60-char names are insufficient (deep chains,
long Go symbols, sub-2% hotspots), export the complete profile — nothing is
truncated there, and the diff is a first-class operation:

```bash
gcx profiles query -d <pyro-uid> '{service_name="<suspect>", <label>="<hot-value>"}' \
  --profile-type <lens> --from <incident-from> --to <incident-to> \
  -o pprof --pprof-path incident.pb.gz

gcx profiles query -d <pyro-uid> '{service_name="<suspect>", <label>="<hot-value>"}' \
  --profile-type <lens> --from <baseline-from> --to <baseline-to> \
  -o pprof --pprof-path baseline.pb.gz

go tool pprof -top -nodefraction=0 incident.pb.gz          # full ranking
go tool pprof -top -base baseline.pb.gz incident.pb.gz     # ranked by GROWTH
go tool pprof -lines -top -cum incident.pb.gz              # file:line per function
```

## Step 6: Pin the source location (conditional)

Only when a repository checkout is available and matches the service: compare
`git remote get-url origin` against the service's `service_repository` label
(visible in the Step-4b output). If they match:

- Take `file:line` per hot function from `go tool pprof -lines -top -cum`.
- If the profile's Build ID contains a `git_ref`, compare it with `git log
  --oneline -1`; if they differ, warn that line numbers may be shifted.
- Read the reported lines only to **confirm the mechanism** (a loop, an
  allocation site, a lock) so the report can name it. Do not analyze further
  and do not propose fixes — locating is the deliverable.

If the repo does not match or no checkout exists, skip silently; the
function-level location from Step 5 stands.

## Optional: correlate with a specific request

When the trigger was a single slow trace (ID from Tempo or logs), scope the
profile to it instead of the slice — same reading rules apply:

```bash
gcx profiles query -d <pyro-uid> '{service_name="<suspect>"}' \
  --profile-type <lens> --from <incident-from> --to <incident-to> \
  --trace-id <trace-id>
```

An empty result here does not mean the trace was cheap: trace/span scoping
requires span-aware instrumentation (e.g. Go's otelpyroscope). Check
`gcx profiles exemplars span ...` returns span IDs before concluding anything
from an empty scoped query.

## Step 7: The location report (always produced)

```
Service:        <service-name>
Lens:           <profile type> (chosen because <symptom>)
Slice:          <label>=<value> (<N>x the next slice) | uniform fleet-wide
Window:         <incident window> vs baseline <baseline window>
Location:       <application function>  <file:line if pinned>
Share:          <X>% of the slice's <resource> in the incident window
                (<Y>% in baseline — <grew/appeared/unchanged>)
Mechanism:      <one line: busy-wait / allocation site / lock wait / ...>
Confidence:     <high|medium|low> — <one line why>
Not determined: <anything the gate or empty branches ruled out, or "-">
```

If the funnel ended in a bounded negative, the report still ships: Location
becomes "not in this service's profiled code", Mechanism explains what was
ruled out (e.g. "CPU and alloc uniform and flat vs baseline; cost is likely
off-CPU or external"), and the handoff is `debug-with-grafana`.

Fix proposals are out of scope by design — hand the location to the owner.
