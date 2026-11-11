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

Use `gcx assistant usage get` to check how much of your monthly Grafana Assistant allowance you've used. Administrators also see shared usage for the stack and the users who are close to their token limit.

## Before you begin

- [Configure a gcx context](../../configuration/) for a Grafana Cloud stack with Grafana Assistant enabled.
- To see organization usage, make sure your identity has the `grafana-assistant-app.usage:read` permission.

The command uses your selected context. Add `--context CONTEXT` to select another context.

## View your usage

To view your usage, run:

```shell
gcx assistant usage get
```

The `YOUR USAGE` section shows only your personal counters:

- Prompts.
- Tokens, when Grafana reports your own token usage. If your token limit belongs to a shared bucket, no personal token count appears.
- Investigations, only when Grafana reports them for you. Investigations currently share a stack-wide limit, so they usually appear in the organization section instead.

For each counter with a limit, the output shows the count, a usage bar, the percentage used, and the remaining allowance. Counters are sorted by percentage used, and counters without a limit appear last. Usage above 100% shows the actual percentage, and the bar stays full.

In an interactive terminal, bars are blue, turn amber at 80%, and turn red at the limit. When you pipe the output or use `--no-color`, bars use ASCII characters.

If you don't have administrator access and a shared limit is reached, a short notice appears. Shared counters aren't shown as your own usage.

### View organization usage

If your identity has the `grafana-assistant-app.usage:read` permission, the command also shows an `ORGANIZATION` section for the selected stack. This section covers only the selected stack, not every stack in the organization. It includes:

- Stack token usage and its limit.
- Shared investigations.
- Service account token pool usage, when reported.

Each shared counter appears once, in this section.

If you don't have the right permissions, the command shows only your usage. Other errors, such as authentication failures, unavailable endpoints, or invalid counters, make the command fail instead of reporting missing values as zero.

### Identify users who need attention

Below the counters, a list shows the users who need attention. These are users at or above 80% of their effective per-user token limit, sorted by percentage used, with users who have reached their limit first. 

The list leaves out:

- Users below 80% of their limit.
- Users without a limit.
- Individual service accounts. Service accounts share a pool, so their individual usage isn't compared against the pool's limit.

If no users are close to their limit, a short message says so.

By default, the list shows up to 10 users and reports the total number who need attention. To show all of them, run:

```shell
gcx assistant usage get --top 0
```

To review per-user limits, open the **Usage** page in Grafana Assistant. If team information is incomplete, the output flags it, because limits that come from teams might also be incomplete.

## Export usage

To export usage as JSON, run:

```shell
gcx assistant usage get -o json
gcx assistant usage get --context my-stack -o json
```

JSON output has the same two sections as the terminal view:

- `yourUsage.metrics` contains your personal counters.
- `orgUsage.metrics` contains shared counters. It's included only when you have administrator access.
- `orgUsage.usersNeedingAttention` lists users at or above 80% of their effective token limit, highest percentage first. If no users match, it's an empty array (`[]`). It leaves out users below 80%, users without a limit, and service accounts.
- `orgUsage.teamDataPartial` is `true` when limits that come from teams might be incomplete.

Each metric has `used`, `limit`, and `month` fields. Counts and limits are in raw units, and a limit of `0` means no limit is reported. For example, `yourUsage.metrics.tokens` describes your token allowance, and `orgUsage.metrics.investigations` describes the shared investigation allowance. Service account token pools, when reported, use the `serviceAccountTokens` key.

User records keep the values that the API returns:

- `usedThisMonthTokens` is a raw token count.
- `effectiveLimitMtok` is in millions of tokens.

The following behavior also applies to exported output:

- `--top` affects only text output. JSON always includes every user who needs attention.
- Agent mode output uses the same structure as JSON.
- YAML output (`-o yaml`) contains the full API response that your identity can access.

To extract only the users who need attention, run:

```shell
gcx assistant usage get --jq '.orgUsage.usersNeedingAttention'
```

## Limits and reporting details

The following applies:

- Usage periods are calendar months in UTC.
- Investigation bars use the effective limit when Grafana reports one, and the base limit otherwise.
- A limit of zero appears as `No cap reported`. This doesn't guarantee unlimited use, because this API doesn't expose prompt limits from legacy pricing.
- Your token counter reflects a single limit, either personal or shared.
- With administrator access, you can see the stack token limit before the stack reaches it.
- These counters describe usage limits. They aren't a billing estimate.
- The command times out after 30 seconds.
- The command detects administrator access automatically, so there are no `--admin` or `--timeout` flags.