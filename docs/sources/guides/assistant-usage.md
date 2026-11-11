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

If you don't have administrator access and a shared limit is reached, a short notice appears. Shared counters aren't shown as your own usage.

### Ask for more when a limit blocks you

When a token limit blocks you, the output says which one and when it resets. If your Assistant Admins accept limit requests, it also shows the command that asks them to raise it:

```shell
gcx assistant limit-requests create user_tokens --note "Debugging the checkout latency alert"
```

Use one of these limits:

- `user_tokens`: your per-user token limit.
- `stack_tokens`: the stack's token limit.
- `active_users`: every Assistant seat on the stack is taken. People without a seat can't read the seat count, so `usage get` shows this block only after you've asked. To ask, run `gcx assistant limit-requests create active_users`.

Grafana accepts a request only while that limit blocks you and an admin can raise it. Your admins get a Grafana notification with your note, which can be up to 280 characters, and you get one when the limit is raised. While your request is pending, `usage get` shows when you asked, and asking again returns the same request. If your admins turned limit requests off, the output asks you to contact them instead.

You can't request more prompts, which come from your plan, or more investigations.

When a limit refuses `gcx assistant prompt`, gcx stops retrying and shows the same block below the error.

Requests come from the blocked person, so a gcx context that uses a service account token can't create one. To ask, sign in as yourself with `gcx login`.

To follow your requests, including ones your admins resolved or dismissed, run:

```shell
gcx assistant limit-requests list --mine
gcx assistant limit-requests list --mine --state pending
```

`gcx assistant limit-requests` is experimental. It needs a stack with Assistant limit requests turned on.

### View organization usage

If your identity has the `grafana-assistant-app.usage:read` permission, the command also shows an `ORGANIZATION` section for the selected stack. This section covers only the selected stack, not every stack in the organization. It includes:

- Seats in use and the seat limit, when reported.
- Stack token usage and its limit.
- Shared investigations.

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

### Review limit requests

If your identity has the `grafana-assistant-app.usage:read` permission, a `PENDING LIMIT REQUESTS` section groups this month's pending requests by the limit setting you would change: the stack's seats or token limit, one team's per-user token grant, or the default per-user token limit. The largest group comes first.

To list the groups, or each request in the matching groups, run:

```shell
gcx assistant limit-requests list
gcx assistant limit-requests list --type user_tokens --team infra
```

The filtered list also shows each request's ID.

To approve requests, raise the limit they ask about. This needs the `grafana-assistant-app.usage:write` permission. Raising it resolves every request the new value unblocks and notifies the people who asked. Give `--to` in seats, or in whole millions of tokens such as `60M`. Use `--dry-run` first to see the current limit and how many requests the new value resolves:

```shell
gcx assistant limit-requests raise active_users --to 15 --dry-run
gcx assistant limit-requests raise user_tokens --team infra --to 60M
```

`user_tokens` without `--team` raises the default per-user limit. gcx keeps your other token limits as they are.

To decline without changing a limit, dismiss one request by ID or a whole group. gcx asks you to confirm unless you pass `--force`:

```shell
gcx assistant limit-requests dismiss 0b6f3c8e-2d4a-4c1e-9f7a-1e2d3c4b5a69
gcx assistant limit-requests dismiss --type user_tokens --team infra
```

Limit requests are off for a stack until an admin turns them on. To check or change that, and the usage alert people get near a limit, run:

```shell
gcx assistant limit-requests settings get
gcx assistant limit-requests settings update --enabled --alert-threshold 90
```

`settings update` changes only the flags you set.

## Export usage

To export usage as JSON, run:

```shell
gcx assistant usage get -o json
gcx assistant usage get --context my-stack -o json
```

JSON output has the same two sections as the terminal view:

- `yourUsage.metrics` contains your personal counters.
- `orgUsage.metrics` contains shared counters, such as shared investigations. It's included for every caller.
- `orgUsage.usersNeedingAttention` lists users at or above 80% of their effective token limit, highest percentage first. It's `null` unless you have administrator access. If no users match, it's an empty array (`[]`). It leaves out users below 80%, users without a limit, and service accounts.
- `orgUsage.teamDataPartial` is `true` when limits that come from teams might be incomplete.
- `yourUsage.limitsReached` lists the limits blocking you that an admin can raise, such as `["user_tokens"]`.
- `yourUsage.pendingLimitRequests` lists your requests that are still waiting. `yourUsage.limitRequestsEnabled` says whether your admins accept requests; it's left out when the stack doesn't support them.
- `orgUsage.pendingLimitRequests` lists this month's request groups when you can review them.

Each metric has `used`, `limit`, and `month` fields. Counts and limits are in raw units, and a limit of `0` means no limit is reported. For example, `yourUsage.metrics.tokens` describes your token allowance, and `orgUsage.metrics.investigations` describes the shared investigation allowance. Seats, when reported, use the `seats` key.

User records keep the values that the API returns:

- `usedThisMonthTokens` is a raw token count.
- `effectiveLimitMtok` is in millions of tokens.

The following behavior also applies to exported output:

- `--top` affects only text output. JSON always includes every user who needs attention.
- Agent mode and YAML output use the same structure as JSON.

To extract only the users who need attention, run:

```shell
gcx assistant usage get --jq '.orgUsage.usersNeedingAttention'
```

## Limits and reporting details

The following applies:

- Usage periods are calendar months in UTC.
- Bars use the effective limit when Grafana reports one, and the base limit otherwise.
- A limit of zero appears as `No cap reported`. This doesn't guarantee unlimited use, because this API doesn't expose prompt limits from legacy pricing.
- Your token counter reflects a single limit, either personal or shared.
- With administrator access, you can see the stack token limit before the stack reaches it.
- These counters describe usage limits. They aren't a billing estimate.
- The command times out after 30 seconds.
- The command detects administrator access automatically, so there are no `--admin` or `--timeout` flags.