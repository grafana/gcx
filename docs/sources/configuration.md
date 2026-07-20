---
title: Configure gcx
labels:
  products:
    - cloud
    - enterprise
    - oss
weight: 3
---

# Configure `gcx`

You can configure `gcx` with a configuration file or using environment variables.

- A configuration file can store multiple contexts, which makes it easier to switch between Grafana instances. Check the [configuration file reference documentation](https://github.com/grafana/gcx/tree/main/docs/reference/configuration/index.md) for details on all available configuration options.
- Environment variables describe a single context, so they work best in CI environments. Refer to [Configure `gcx` with environment variables ](#configure-gcx-with-environment-variables) for more information. 

## Choose an authentication method

`gcx` supports three ways to authenticate to Grafana:

- **OAuth** (Grafana Cloud only): Browser-based sign-in with `gcx login`. Recommended for interactive use. The tokens are user-scoped: every request runs with your own identity and RBAC permissions, so you can't access anything through `gcx` that you can't already access in the Grafana UI. Refer to [Required role for OAuth sign-in](#required-role-for-oauth-sign-in) for the permission this flow needs.
- **Service account token**: Works for Grafana Cloud and on-premises instances, and is the recommended method for CI and other non-interactive environments. Refer to [Grafana service accounts](https://grafana.com/docs/grafana/latest/administration/service-accounts/) for how to create one.
- **Basic authentication**: Username and password. Use this only when service accounts aren't available.

### Required role for OAuth sign-in

To authorize a `gcx` CLI connection with OAuth, your Grafana user needs the `grafana-assistant-app.tokens.gcx:access` permission. The **gcx User** role, registered by the Grafana Assistant application, grants this permission and is assigned automatically to users with the basic role Viewer or higher.

This permission only lets you create `gcx` tokens for your own user. It doesn't grant access to other users' tokens and it doesn't extend your existing Grafana permissions.

{{< admonition type="note" >}}
If `gcx login` fails with a `Permission Required` error naming the **gcx User** role, ask your Grafana administrator to assign you the **gcx User** role, or a custom role that includes the `grafana-assistant-app.tokens.gcx:access` permission. If the role doesn't exist on your instance, the Grafana Assistant application needs to be updated to a version that includes it.
{{< /admonition >}}

## Understand the `gcx` configuration file in use

Run `gcx config check` to display the configuration file currently in use.

`gcx` stores its configuration in a YAML file. Configuration is prioritized in this order:

1. If the `--config` flag is set, then that file will be loaded. No other location will be considered.
2. If the `$XDG_CONFIG_HOME` environment variable is set, then it will be used: `$XDG_CONFIG_HOME/gcx/config.yaml`
3. If the `$HOME` environment variable is set, then it will be used: `$HOME/.config/gcx/config.yaml`
4. If the `$XDG_CONFIG_DIRS` environment variable is set, then it will be used: `$XDG_CONFIG_DIRS/gcx/config.yaml`

## Define contexts

`gcx` supports multiple contexts so you can switch between instances. A context references a named stack entry, which holds the Grafana connection details. By default, `gcx` uses the `default` context.

To configure the `default` context:

```shell
gcx config set stacks.default.grafana.server http://localhost:3000
gcx config set contexts.default.stack default

# Set org-id when using OSS/Enterprise - skip when targeting Grafana Cloud
gcx config set stacks.default.grafana.org-id 1

# Authenticate with a service account token
gcx config set stacks.default.grafana.token service-account-token

# Or alternatively, use basic authentication
gcx config set stacks.default.grafana.user admin
gcx config set stacks.default.grafana.password admin
```

To create another context, use the same pattern:

```shell
gcx config set stacks.staging.grafana.server https://staging.grafana.example
gcx config set stacks.staging.grafana.org-id 1
gcx config set contexts.staging.stack staging
```

Note that in these examples, `default` and `staging` are the context and stack names.

## Useful commands

Use these commands to check the configuration:

```shell
gcx config check
```

List existing contexts:

```shell
gcx config list-contexts
```

Switch to a different context:

```shell
gcx config use-context staging
```

See the entire configuration:

```shell
gcx config view
```

## Configure `gcx` with environment variables 

Every supported environment variable is listed in our [reference documentation](https://github.com/grafana/gcx/tree/main/docs/reference/environment-variables/index.md). 

Since `gcx` connects to Grafana through the REST API, you must configure authentication credentials. At minimum, set the Grafana URL and organization ID:

```shell
GRAFANA_SERVER='http://localhost:3000' GRAFANA_ORG_ID='1' gcx config check
```

Depending on your authentication method, also set one of the following:

- If you use a [Grafana service account](https://grafana.com/docs/grafana/latest/administration/service-accounts/) (recommended), set a [token](https://github.com/grafana/gcx/tree/main/docs/reference/environment-variables/index.md#grafana_token).
- If you use basic authentication, set a [username](https://github.com/grafana/gcx/tree/main/docs/reference/environment-variables/index.md#grafana_user) and a [password](https://github.com/grafana/gcx/tree/main/docs/reference/environment-variables/index.md#grafana_password).

After you configure authentication, you can start using `gcx`.

If you want to persist this configuration, [create a context](#define-contexts).