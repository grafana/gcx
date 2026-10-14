## gcx frontend apps create

Create a Frontend Observability app from a file.

### Synopsis

Create a Frontend Observability app from a file.

Set spec.appType and spec.runtime at creation; the API ignores later changes
to appType. Web apps use appType web with runtime web-js. Mobile apps use
appType mobile with runtime flutter, react-native, android-native, or
swift-native. Create sends spec.extraLogLabels, including the legacy is_mobile
label.

Create and update send spec.settings. Set geolocationLevel to continent,
country, subdivision, city, or network. Set geolocationCountryDenylist to ISO
country codes, such as [DE], to skip enrichment for those sessions.

```
gcx frontend apps create [flags]
```

### Examples

```
  # Create an app from a YAML file.
  gcx frontend apps create -f app.yaml

  # Create a native Android app from stdin.
  cat <<EOF | gcx frontend apps create -f -
  apiVersion: faro.ext.grafana.app/v1alpha1
  kind: FaroApp
  metadata:
    name: my-mobile-app
  spec:
    name: my-mobile-app
    appType: mobile
    runtime: android-native
  EOF
```

### Options

```
  -f, --filename string   File containing the Frontend Observability app manifest (use - for stdin)
  -h, --help              help for create
      --jq string         jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string       Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string     Output format. One of: agents, json, text, yaml (default "text")
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from known agent identity variables. Set GCX_AGENT_NAME to identify a supported harness, or GCX_AGENT_MODE to control the mode.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Requires -vvv. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx frontend apps](gcx_frontend_apps.md)	 - Manage Frontend Observability apps.

