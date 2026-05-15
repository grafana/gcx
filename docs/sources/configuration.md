---
title: Configure `gcx`
---

# Configure `gcx`

You can configure `gcx` either with environment variables or with a configuration file:

- Environment variables describe a single context, so they work best in CI environments.
- A configuration file can store multiple contexts, which makes it easier to switch between Grafana instances.

## Environment variables

Since `gcx` connects to Grafana through the REST API, you must configure authentication credentials.

At minimum, set the Grafana URL and organization ID:

```shell
GRAFANA_SERVER='http://localhost:3000' GRAFANA_ORG_ID='1' gcx config check
```

Depending on your authentication method, also set one of the following:

- A [token](../reference/environment-variables/index.md#grafana_token) if you use a [Grafana service account](https://grafana.com/docs/grafana/latest/administration/service-accounts/) (recommended).
- A [username](../reference/environment-variables/index.md#grafana_user) and [password](../reference/environment-variables/index.md#grafana_password) if you use basic authentication.

If you want to persist this configuration, [create a context](#define-contexts).

After you configure authentication, you can start using `gcx`.

The following applies:

- Every supported environment variable is listed in our [reference documentation](../reference/environment-variables/index.md).
- Check the [config file reference documentation](../reference/configuration/index.md) for details on all available config options.

### Define contexts

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

## Configuration file

`gcx` stores its configuration in a YAML file. It looks for the file in this order:

1. If the `--config` flag is set, then that file will be loaded. No other location will be considered.
2. If the `$XDG_CONFIG_HOME` environment variable is set, then it will be used: `$XDG_CONFIG_HOME/gcx/config.yaml`
3. If the `$HOME` environment variable is set, then it will be used: `$HOME/.config/gcx/config.yaml`
4. If the `$XDG_CONFIG_DIRS` environment variable is set, then it will be used: `$XDG_CONFIG_DIRS/gcx/config.yaml`

Run `gcx config check` to display the configuration file currently in use.

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
