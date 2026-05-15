---
title: Configure gcx
weight: 3
---

# Configure `gcx`

You can configure `gcx` with a configuration file or using environment variables.

- A configuration file can store multiple contexts, which makes it easier to switch between Grafana instances. Check the [configuration file reference documentation](https://github.com/grafana/gcx/tree/main/docs/reference/configuration/index.md) for details on all available configuration options.
- Environment variables describe a single context, so they work best in CI environments. Refer to [Configure `gcx` with environment variables ](#configure-gcx-with-environment-variables) for more information. 

## Understand the `gcx` configuration file in use

Run `gcx config check` to display the configuration file currently in use.

`gcx` stores its configuration in a YAML file. Configuration is prioritized in this order:

1. If the `--config` flag is set, then that file will be loaded. No other location will be considered.
2. If the `$XDG_CONFIG_HOME` environment variable is set, then it will be used: `$XDG_CONFIG_HOME/gcx/config.yaml`
3. If the `$HOME` environment variable is set, then it will be used: `$HOME/.config/gcx/config.yaml`
4. If the `$XDG_CONFIG_DIRS` environment variable is set, then it will be used: `$XDG_CONFIG_DIRS/gcx/config.yaml`

## Define contexts

`gcx` supports multiple contexts so you can switch between instances. By default, it uses the `default` context.

To configure the `default` context:

```shell
gcx config set contexts.default.grafana.server http://localhost:3000

# Set org-id when using OSS/Enterprise - skip when targeting Grafana Cloud
gcx config set contexts.default.grafana.org-id 1

# Authenticate with a service account token
gcx config set contexts.default.grafana.token service-account-token

# Or alternatively, use basic authentication
gcx config set contexts.default.grafana.user admin
gcx config set contexts.default.grafana.password admin
```

To create another context, use the same pattern:

```shell
gcx config set contexts.staging.grafana.server https://staging.grafana.example
gcx config set contexts.staging.grafana.org-id 1
```

Note that in these examples, `default` and `staging` are the context names.

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