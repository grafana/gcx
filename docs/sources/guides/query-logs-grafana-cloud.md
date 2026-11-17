---
title: Query logs on Grafana Cloud
labels:
  products:
    - cloud
description: Understand the Loki query cost check on Grafana Cloud, and what to do when gcx blocks a query.
keywords:
  - gcx
  - Loki
  - logs
  - query cost
weight: 25
---

# Query logs on Grafana Cloud

On Grafana Cloud, `gcx` estimates the cost of a Loki query, written in [LogQL](https://grafana.com/docs/loki/latest/query/), before it runs it. If the query is too expensive and your stack enforces a budget, `gcx` blocks it so you don't scan more log data than you need.

The check applies to these commands:

- [`gcx logs query`](../../cli-reference/#gcx-logs-query)
- [`gcx logs metrics`](../../cli-reference/#gcx-logs-metrics)
- [`gcx datasources loki query`](../../cli-reference/#gcx-datasources-loki-query)
- [`gcx datasources loki metrics`](../../cli-reference/#gcx-datasources-loki-metrics)

Self-managed Grafana targets aren't checked.

## How the check works

`gcx` blocks the query if any of the following is true:

- The [stream selector](https://grafana.com/docs/loki/latest/query/log_queries/#log-stream-selector) doesn't narrow the streams. Every selector needs at least one positive matcher, such as `app="api"`. `gcx` blocks selectors such as `{}`, `{job=~".+"}`, or selectors with only negative matchers, whatever their size.
- The time range is longer than the stack's time range limit. The time range includes any [range vector](https://grafana.com/docs/loki/latest/query/metric_queries/#range-vector-aggregation) in the query, such as `[30d]`.
- The estimated scan is larger than the stack's byte budget. `gcx` estimates the scan with [Loki index statistics](https://grafana.com/docs/loki/latest/reference/loki-http-api/#query-log-statistics).

`gcx logs metrics` with no time flags runs an instant query. `gcx` estimates it over the last minute, plus any range in the query.

If `gcx` can't parse the query, it sends it to Loki, which reports the syntax error.

## Who sets the budget

A stack administrator sets the budget and the mode in the Grafana Assistant settings. The mode decides what `gcx` does with a query that's over the budget:

| Mode | What happens |
| --- | --- |
| `enforce` | `gcx` blocks the query. |
| `shadow` | `gcx` runs the query and logs that it would have blocked it. |
| `off` | `gcx` doesn't check the query. |

If your stack doesn't serve a policy, or `gcx` can't fetch it, `gcx` uses `shadow` mode with a 50 GiB budget and a 24-hour time range limit. In this case, `gcx` never blocks a query.

## Fix a blocked query

When `gcx` blocks a query, the command fails with a `Query blocked` error and exit code `1`. The error shows the reasons, the estimated scan and budget if `gcx` measured the scan, and how to narrow the query.

To fix a blocked query, try the following:

1. Narrow the stream selector. To find specific labels to match on, run:

   ```shell
   gcx logs labels
   ```

1. Shorten the time range with `--since`, or with `--from` and `--to`:

   ```shell
   gcx logs query '{namespace="checkout", app="api"} |= "error"' --since 1h
   ```

1. If you need the wide query, run it with `--force`:

   ```shell
   gcx logs query '{namespace="checkout"}' --since 7d --force
   ```

`--force` is the only way to skip the check. The `GCX_AUTO_APPROVE` environment variable doesn't skip it.

To learn more about writing efficient queries, refer to [Query best practices](https://grafana.com/docs/loki/latest/query/bp-query/).

## When the cost estimate fails

If `gcx` can't estimate the cost, for example because Loki index statistics aren't available, the result depends on the mode and on how you run `gcx`:

- **`enforce` mode, in a terminal:** `gcx` warns you and runs the query.
- **`enforce` mode, in agent mode:** `gcx` blocks the query. Retry it, check the data source with `gcx datasources get <UID>`, or pass `--force`.
- **`shadow` mode:** `gcx` runs the query and logs that it couldn't check it.
