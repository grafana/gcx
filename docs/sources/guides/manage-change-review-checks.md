---
title: Manage Assistant change review checks
description: Use gcx to list, tune, create, and delete the checks Grafana Assistant change review runs on GitHub pull requests, and to see how each check performs.
labels:
  products:
    - cloud
  stage: experimental
keywords:
  - gcx
  - assistant
  - change review
  - github
weight: 100
---

# Manage Assistant change review checks

Grafana Assistant change review reviews GitHub pull requests with a set of checks. Use `gcx` to tune the built-in checks, add your own, and see which checks find real issues. **Change review commands are experimental and require Grafana Cloud**.

## Before you begin

- [Configure a gcx context](../../configuration/) for a Grafana Cloud stack with GitHub connected to Grafana Assistant and change review checks available.
- Reading checks and statistics requires the `grafana-assistant-app.github:read` action. Saving checks requires `grafana-assistant-app.github:write`.

## Review the checks

```shell
gcx assistant change-review checks list
gcx assistant change-review checks get security
```

`list` shows every built-in check with the stack's settings, followed by custom checks. Add `-o wide` to see where each check applies and its repository overrides.

## Tune a check

`update` changes only the fields you set:

```shell
# Fail the GitHub Check on a confirmed finding
gcx assistant change-review checks update security --blocking

# Turn a check off
gcx assistant change-review checks update performance --enabled=false

# Run a check only when matching files change
gcx assistant change-review checks update performance --path 'api/**'
```

On a built-in check, `--instructions` adds to the check's own guidance. To edit everything, including repository overrides, round-trip the check through a file:

```shell
gcx assistant change-review checks get security > security.yaml
# edit security.yaml
gcx assistant change-review checks update security -f security.yaml
```

## Add and remove custom checks

```shell
gcx assistant change-review checks create --name "Migration safety" \
  --instructions "Flag schema migrations that lock large tables." --path 'migrations/**'

gcx assistant change-review checks delete migration-safety
```

The check ID comes from its name. Built-in checks can't be deleted. To stop one from running, use `update CHECK --enabled=false`.

Saves are guarded by a revision. If someone else saves the checks at the same time, `gcx` applies your change to the latest checks rather than overwriting theirs.

## See how checks perform

```shell
gcx assistant change-review stats --since 7d
gcx assistant change-review checks list-pull-requests security --repository grafana/gcx
```

`stats` counts, for each check, the issues it confirmed and how many a later review found fixed or still present. `list-pull-requests` shows the pull requests behind one check's counts. Both commands default to the last 30 days and read at most 90 days. They return counts, never finding text.
