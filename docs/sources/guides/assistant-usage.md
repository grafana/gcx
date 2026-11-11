---
title: Check Assistant usage and limits
description: Display your monthly Grafana Assistant limits and automatically surface usage needing administrator attention.
labels:
  products:
    - cloud
keywords:
  - gcx
  - assistant
  - usage
weight: 110
---

# Check Assistant usage and limits

Use your configured Grafana Cloud context to read a current usage snapshot:

```shell
gcx assistant usage get
```

`YOUR USAGE` appears first. Capped counters show compact counts, a bar, the percentage used, and the remaining allowance. Limits are sorted by percentage used, with uncapped counters last. Rows have uniform text styling. Interactive terminals use blue bars, amber at 80%, and red at the limit. Piped output and `--no-color` use ASCII bars. Percentages above 100% remain visible while the bar stays full.

Your usage includes only personal counters: prompts, tokens when the server reports your own token usage, and investigations only if the server explicitly reports them for you. There is no scope column. Investigations currently share a stack-wide cap and appear under **Organization** for admins. Other users see a short notice when a shared limit is reached, without shared counters being presented as their own usage. When the token endpoint reports a shared bucket instead of your own usage, the view does not invent a personal token count.

## Automatic administrator view

When your identity has `grafana-assistant-app.usage:read`, the same command also shows an `ORGANIZATION` section for the **selected stack**, including its authoritative token usage and cap, shared investigations, and any reported service-account pool usage. This is not a total across all stacks in the organization. Each shared bucket appears once, under **Organization**.

The attention list shows only users at or above **80% of their effective per-user token cap**, sorted by percentage used. Users who have reached their limit appear first. Healthy users, uncapped users, and individual service accounts are omitted. Service accounts share a pool, so their individual usage is not compared against that pool's cap. A quiet stack gets a short message that no users are near their reported cap, rather than a leaderboard.

By default, the text list shows at most 10 users and reports how many need attention in total. To display all users needing attention:

```shell
gcx assistant usage get --top 0
```

Review per-user limits in Grafana Assistant's **Usage** page. Incomplete team information is flagged because team-derived limits may be incomplete.

An admin endpoint permission denial (403) produces just your usage. JSON omits `orgUsage` when admin access is unavailable. Other errors, including authentication failures, unavailable endpoints, and malformed counters, fail the command instead of displaying missing data as zero.

## Scope and reporting details

Periods are calendar months in UTC. Investigation bars use `effectiveLimit` when available, otherwise `limit`. A zero limit is labeled `No cap reported`; this does not guarantee unrestricted use, because legacy pricing prompt caps are not exposed by this API. The caller token endpoint reports one applicable scope. Admin access also exposes the stack token cap before it is reached. These counters describe usage limits, not a billing estimate.

## Export usage

```shell
gcx assistant usage get -o json
gcx assistant usage get --context my-stack -o json
```

JSON mirrors the terminal's two sections:

- `yourUsage.metrics` contains your personal counters.
- `orgUsage.metrics` contains shared counters and is included only for admins.
- `orgUsage.usersNeedingAttention` is an array of users at or above 80% of their effective token cap, highest percentage first. No matches returns `[]`. It excludes healthy users, uncapped users, and service accounts.

Each metric has `used`, `limit`, and `month`. Counts and limits are raw units; zero means no reported cap. For example, `yourUsage.metrics.tokens` describes your token allowance and `orgUsage.metrics.investigations` describes the shared investigation allowance. Service-account token pools, when reported, use `serviceAccountTokens`.

`--top` affects only text; JSON always includes all users needing attention. Agent output uses the same structure. `orgUsage.teamDataPartial`, when true, indicates incomplete team-derived limits. Member records retain exact API values: `usedThisMonthTokens` is a raw token count and `effectiveLimitMtok` is millions of tokens. YAML (`-o yaml`) retains the full authorized API snapshot.

To extract just the users needing attention:

```shell
gcx assistant usage get --jq '.orgUsage.usersNeedingAttention'
```

The complete fetch has a fixed 30-second timeout. Admin access is detected automatically; there are no `--admin` or `--timeout` options.
