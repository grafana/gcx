---
title: Dashboards as code
labels:
  products:
    - cloud
    - enterprise
    - oss
description: Use gcx to manage dashboards as code.
keywords:
  - gcx
  - Grafana CLI
  - observability as code
weight: 1
---

# Dashboards as code `gcx` user guide

With this workflow, you can define and manage dashboards as code, saving them to a version control system like Git. This is useful for teams that want to maintain a history of changes, collaborate on dashboard design, and ensure consistency across environments.

## Before you begin

In order to use the `gcx dev serve` functionality, you need to enable a feature toggle in your configuration file:

```ini
[feature_toggles]
kubernetesDashboards = true
```

Refer to [Configure Grafana](https://grafana.com/docs/grafana/latest/setup-grafana/configure-grafana/#feature_toggles) to learn more about enabling feature toggles.

Although the local preview proxy supports service account token, Basic, and mTLS contexts, use a service account token instead of OAuth for `gcx dev serve`. OAuth is rejected before startup until refresh-token rotation can be persisted safely by the long-running server.

## Workflow

To manage dashboards as code with `gcx` follow these steps:

1. Use a dashboard generation script (for example, with the [Foundation SDK](https://github.com/grafana/grafana-foundation-sdk)). You can find an example implementation in the Grafana as code [hands-on lab repository](https://github.com/grafana/dashboards-as-code-workshop/tree/main/part-one-golang-starter).
1. Serve and preview the output of the dashboard generator locally:
   ```shell
   gcx config use-context YOUR_CONTEXT  # for example "dev"
   gcx dev serve --script 'go run scripts/generate-dashboard.go' --watch './scripts'
   ```
1. When the output looks correct, generate dashboard manifest files:
   ```shell
   go run scripts/generate-dashboard.go --generate-resource-manifests --output './resources'
   ```
1. Push the generated resources to your Grafana instance:
   ```shell
   gcx config use-context YOUR_CONTEXT  # for example "dev"
   gcx resources push -p ./resources/
   ```

## Learn more

GIT SYNC TBA