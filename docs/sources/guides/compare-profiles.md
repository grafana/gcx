---
title: Compare profiles
labels:
  products:
    - cloud
    - enterprise
    - oss
description: Compare before and after profile costs and share Profiles Drilldown links with gcx.
keywords:
  - gcx
  - profiling
  - Pyroscope
weight: 6
---

# Compare profiles with `gcx`

Use `gcx profiles query` for one profile and `gcx profiles diff` for symbol costs
across selectors, time windows, or both. Diff uses Pyroscope's server-side
comparison, not independently truncated top-function lists.

## Select the profiles

Configure a Grafana context with read access to Pyroscope. Find the datasource UID
with `gcx datasources list --type pyroscope` and the profile type with
`gcx profiles list-profile-types -d <datasource-uid>`, then compare:

```shell
gcx profiles diff '{service_name="frontend",version="v1"}' '{service_name="frontend",version="v2"}' \
  -d <datasource-uid> --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds \
  --before-from now-1h --before-to now \
  --after-from now-1h --after-to now --drilldown-link
```

Both sides share a datasource and profile type. Repeat the same selector to
compare different windows. All four bounds are required: RFC3339, Unix seconds,
or relative times. They use one clock snapshot and resolve to whole seconds to
match Drilldown. Use fixed timestamps for reproducible evidence.

## Interpret costs

- `self` attributes cost directly to a function; `total` includes callees.
  Recursive inclusive costs count only the outermost occurrence per stack.
- Costs use the profile type's raw sample unit. `delta` is after minus before,
  without duration or traffic normalization. Prefer comparable, equal-length
  windows: traffic, replicas, and sampling can change cost without a regression.
- `changePercent` is null for zero baselines or when either profile has no
  positive samples. Missing samples do not establish zero cost or improvement.
- `complete=false` means frames were folded into `other`; `omitted` reports their
  costs. Raise `--max-nodes` or narrow selectors to improve attribution. Absence
  from a pruned graph does not establish an added or removed function.

Table/wide output shows the top 20 absolute self-cost changes. JSON, YAML, and
agents output retain all returned symbols, warnings, selections, units, and links.
The backend node budget defaults to 50000 in every format.

## Share evidence

`links.before` and `links.after` open individual profiles. `links.explore` opens
before on the left and after on the right, with independent ranges.
`links.comparison` opens the dedicated diff view when representable.

- `--share-link` / `--open`: print or open split Explore.
- `--drilldown-link` / `--open-drilldown`: print or open Profiles Drilldown.

Dedicated comparisons require the same exact `service_name`. Names requiring
string escaping fall back to Explore; other matchers are preserved separately.
Unsupported selectors warn and fall back without dropping scope. Drilldown must
be enabled; URLs target its Scenes contract used in version 3.1.0. Use Explore
for older apps that do not support that contract.

For a profile UUID from exemplars, add `--profile-id <uuid> --drilldown-link` to
`gcx profiles query`. A single ID opens the scoped flame graph; multiple IDs or
stacktrace selectors fall back to Explore. Trace-ID-scoped queries suppress
links because neither UI can preserve their scope.
