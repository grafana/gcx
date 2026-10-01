---
aliases:
  - /docs/grafana-cloud/as-code/observability-as-code/grafana-cli/gcx/overview/
title: Introduction to gcx
labels:
  products:
    - cloud
    - enterprise
    - oss
weight: 1
---

# Overview of the `gcx` CLI

`gcx` is a single CLI that allows you and your AI coding agent structured access to both Grafana (dashboards, folders, alert rules, data sources) and Grafana Cloud products such as Synthetic Monitoring, K6, Fleet Management, Incidents, or Adaptive Telemetry.

`gcx` ships with a suite of agent skills for common workflows like alert investigation, root-cause analysis, dashboard creation and GitOps, SLO management, and observability setup. It natively supports agentic workflows and it's integrated with Grafana Assistant, combining the previously fragmented user experience into one single tool.

`gcx` is under continuous development. [Contact Grafana](https://grafana.com/help/) for support or to report any issues you encounter and help us improve this feature.

## Benefits of `gcx`

Among others, `gcx` provides the following benefits:

- **Manage Grafana OSS/Enterprise and Grafana Cloud:** Use a single tool for dashboards, alerting, SLOs, on-call, synthetic checks, load testing, and more.
- **GitOps**: Pull resources to files, version in Git, or push back with full round-trip fidelity.
- **SRE**: Ensure system performance by monitoring telemetry and root-causing incidents.
- **Observability as code:** `gcx` can scaffold Go projects, import existing dashboards, lint with Rego rules, or live-reload development servers.
- **Automation:** `gcx` uses JSON/YAML output, structured errors, and predictable exit codes.
- **Multi-environment:** Use named contexts to switch between development, staging, and production environments.
- **AI agent friendly:** Agent mode auto-detected for Claude Code, Copilot, Cursor, and other.

## Costs

{{< admonition type="note" >}}

`gcx` itself is free, but **some commands operate Grafana Cloud products that are billed based on usage**. 

{{< /admonition >}}

For example:

- Grafana Assistant is charged per token consumed, including requests made through `gcx`. 
- Synthetic Monitoring is billed per test execution. 
- Performance Testing (k6) is charged per Virtual User Hour. 
- IRM is billed per monthly active user. 

For details, refer to the [Cost Management and Billing documentation](https://grafana.com/docs/grafana-cloud/cost-management-and-billing/) and [Grafana Cloud pricing](https://grafana.com/pricing/).

## Compatibility

The following applies:  

- `gcx` is available for Grafana Cloud and Grafana OSS/Enterprise v12 or later. Older Grafana versions are not supported. 

- `gcx` is compatible with any agentic coding tool.

- `gcx` works across a wide range of Grafana product offerings. Feature availability depends on your Grafana deployment. For more information, refer to the [Compatibility matrix](https://github.com/grafana/gcx#compatibility).

## CLI command reference

You can find the up-to-date command reference guide in the [CLI command reference](https://github.com/grafana/gcx/tree/main/docs/reference/cli) in GitHub.

### Configure `gcx`

Refer to [Configuration commands](https://grafana.com/docs/grafana/latest/as-code/observability-as-code/grafana-cli/gcx/configuration/#useful-commands) for an overview of useful commands to check your configuration.

## Experimental commands

Commands are stable within a major version, with the exception of those labelled experimental.

However, some commands are labelled `[experimental]` in their help text and carry a `stability` field of `experimental` in `gcx commands` output. Commands are labelled experimental when they are not yet stable, or when they operate a Grafana Cloud feature that is not yet Generally Available. For more information, refer to [Release life cycle for Grafana Labs](https://grafana.com/docs/release-life-cycle/). **An experimental command may be removed, or its subcommands, flags, and responses may change, without following the normal semantic versioning conventions**. 

## Manage your resources

You can manage your resources using the `gcx resources` set of commands. Refer to the [Resource Model guide](https://github.com/grafana/gcx/blob/main/docs/architecture/resource-model.md) for more information on the architecture and resource model used by `gcx`.

### Work with resources from other tools 

If you want to work with resources managed by other tools, such as Terraform or Git Sync, use the flag `--include-managed` with commands such as [`gcx resources pull`](https://github.com/grafana/gcx/blob/main/docs/reference/cli/gcx_resources_pull.md) or [`gcx resources push`](https://github.com/grafana/gcx/blob/main/docs/reference/cli/gcx_resources_push.md).

### Extract dashboards

At the moment, if you extract a dashboard as a JSON (`gcx resources pull dashboards -o json`), `gcx` only returns the original JSON, and can't convert it to a different version.

## Migrate from `grafanactl`

If you want to migrate from `grafanctl` to `gcx`, search-and-replace `grafanactl` with `gcx`. For `grafanactl resources serve`, use `gcx dev serve` instead.

## Learn more

Refer to the [`gcx` repository](https://github.com/grafana/gcx) in GitHub for the full set of `gcx` documents, including more information on:

- Installation and configuration
- How to manage resources, including dashboards-as-code
- Architecture
- User guides