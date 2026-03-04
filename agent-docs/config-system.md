# Configuration and Context System

## Overview

grafanactl uses a context-based multi-environment configuration model directly
inspired by kubectl's kubeconfig. A single YAML file stores named contexts,
each pointing to a different Grafana instance. One context is "current" at any
time, and all commands operate against it unless overridden.

All code lives under `internal/config/` and `cmd/grafanactl/config/command.go`.

---

## Data Model

```
Config
├── Source          (runtime-only: path of loaded file)
├── CurrentContext  "production"
└── Contexts        map[string]*Context
    ├── "production"
    │   ├── Grafana  *GrafanaConfig
    │   │   ├── Server    "https://grafana.example.com"
    │   │   ├── User      ""
    │   │   ├── Password  ""            // datapolicy:"secret"
    │   │   ├── APIToken  "glsa_..."    // datapolicy:"secret"  (takes precedence over User/Password)
    │   │   ├── OrgID     0             // on-prem: org namespace
    │   │   ├── StackID   12345         // cloud: stack namespace
    │   │   └── TLS       *TLS
    │   │       ├── Insecure    false
    │   │       ├── ServerName  ""
    │   │       ├── CertData    []byte   // datapolicy:"secret" on KeyData
    │   │       ├── KeyData     []byte
    │   │       ├── CAData      []byte
    │   │       └── NextProtos  []string
    │   ├── DefaultPrometheusDatasource  ""   // UID of default Prometheus datasource for queries
    │   ├── DefaultLokiDatasource        ""   // UID of default Loki datasource for queries
    │   └── Providers  map[string]map[string]string
    │       ├── "slo"       {"url": "...", "token": "..."}   // secret keys REDACTED in config view
    │       └── "oncall"    {"url": "..."}
    └── "staging"
        └── Grafana  *GrafanaConfig
            └── ...
```

Source files:
- `internal/config/types.go` — all struct definitions (`Config`, `Context`, `GrafanaConfig`, `TLS`)
- `internal/config/errors.go` — `ValidationError`, `UnmarshalError`, `ContextNotFound`

### Comparison to kubectl kubeconfig

| kubectl kubeconfig | grafanactl config | Notes |
|--------------------|-------------------|-------|
| `clusters[]`       | `contexts[].grafana.server` | grafanactl merges cluster+auth into one context |
| `users[]`          | `contexts[].grafana.{user,password,token}` | No separate user objects |
| `contexts[]`       | `contexts{}` (map) | grafanactl uses a map, kubectl uses a list |
| `current-context`  | `current-context` | Identical concept |
| `namespace` in context | derived from org-id/stack-id | See Namespace Semantics below |

Key difference: kubectl separates clusters, users, and contexts into three
separate lists to allow reuse. grafanactl collapses all three into a single
`Context` entry, which is simpler but means auth+server are always paired.

---

## Annotated Config File Example

```yaml
# ~/.config/grafanactl/config.yaml

current-context: "production"   # which context is active

contexts:
  # On-prem Grafana (uses org-id as namespace)
  production:
    grafana:
      server: "https://grafana.example.com"
      token: "glsa_xxxx"          # API token — takes precedence over user/password
      org-id: 1                   # maps to namespace "org-1" in K8s API calls
      tls:
        insecure-skip-verify: false
        ca-data: <base64 PEM>     # custom CA bundle (base64-encoded in file)
        cert-data: <base64 PEM>   # client cert (base64-encoded in file)
        key-data: <base64 PEM>    # client key  (base64-encoded in file)

  # Grafana Cloud (uses stack-id as namespace)
  cloud-staging:
    grafana:
      server: "https://myorg.grafana.net"
      token: "glsa_yyyy"
      stack-id: 12345             # maps to namespace "stacks-12345" in K8s API calls
                                  # can be omitted if auto-discovery succeeds (see below)

  # Basic auth, on-prem dev
  local:
    grafana:
      server: "http://localhost:3000"
      user: "admin"
      password: "admin"           # REDACTED in `config view` output
      org-id: 1
```

---

## File Location and Loading Order

### File Location Priority (highest to lowest)

```
1. --config <path>          CLI flag → ExplicitConfigFile(path)
2. $GRAFANACTL_CONFIG       env var  → StandardLocation() checks this first
3. $XDG_CONFIG_HOME/grafanactl/config.yaml
4. $HOME/.config/grafanactl/config.yaml
5. $XDG_CONFIG_DIRS/grafanactl/config.yaml  (e.g., /etc/xdg/...)
```

Source: `internal/config/loader.go:40-64` (`StandardLocation` function) and
`cmd/grafanactl/config/command.go:103-109` (`configSource` method).

Constants defined in `loader.go`:
```go
StandardConfigFolder   = "grafanactl"
StandardConfigFileName = "config.yaml"
ConfigFileEnvVar       = "GRAFANACTL_CONFIG"
configFilePermissions  = 0o600   // file is always written with these perms
```

If no config file exists at the standard location, an empty one is created
automatically with a single `default` context:

```go
// loader.go:23-27
const defaultEmptyConfigFile = `
contexts:
  default: {}
current-context: default
`
```

### Load Function Signature

```go
// internal/config/loader.go:66
func Load(ctx context.Context, source Source, overrides ...Override) (Config, error)

type Override func(cfg *Config) error   // applied in order after YAML decode
type Source  func() (string, error)     // returns the path to read
```

Loading steps (in `Load`, lines 66–98):
1. Call `source()` to get the file path
2. `os.ReadFile` the file
3. YAML-decode with `BytesAsBase64: true` (so `[]byte` fields are stored as base64 in YAML)
4. Post-process: populate `ctx.Name` from the map key for each context
5. Apply each `Override` function in order
6. On `ValidationError`, call `annotateErrorWithSource` to embed a YAML-path-aware source annotation

---

## Environment Variable Overrides

> See also [design-guide.md](design-guide.md) Section 10 for the complete
> environment variable reference (core + provider + planned variables).

Environment variables are applied as an `Override` function during load. They
patch the **current context's** `GrafanaConfig` struct in-place.

Implementation: `cmd/grafanactl/config/command.go:38-62` (`loadConfigTolerant`):

```go
func(cfg *config.Config) error {
    // Ensure current-context and its Grafana sub-struct exist
    if cfg.CurrentContext == "" {
        cfg.CurrentContext = config.DefaultContextName
    }
    if !cfg.HasContext(cfg.CurrentContext) {
        cfg.SetContext(cfg.CurrentContext, true, config.Context{})
    }
    curCtx := cfg.Contexts[cfg.CurrentContext]
    if curCtx.Grafana == nil {
        curCtx.Grafana = &config.GrafanaConfig{}
    }
    // github.com/caarlos0/env/v11 reads struct tags of the form `env:"GRAFANA_SERVER"`
    return env.Parse(curCtx)
}
```

The `env` struct tags on `GrafanaConfig` (`types.go:77–100`) declare the
mapping:

| Env Var           | Config Field              | Type    |
|-------------------|---------------------------|---------|
| `GRAFANA_SERVER`  | `GrafanaConfig.Server`    | string  |
| `GRAFANA_USER`    | `GrafanaConfig.User`      | string  |
| `GRAFANA_PASSWORD`| `GrafanaConfig.Password`  | string  |
| `GRAFANA_TOKEN`   | `GrafanaConfig.APIToken`  | string  |
| `GRAFANA_ORG_ID`  | `GrafanaConfig.OrgID`     | int64   |
| `GRAFANA_STACK_ID`| `GrafanaConfig.StackID`   | int64   |

Key behavior: env vars override the **current context** only. They do not
affect other contexts in the file. The file itself is never mutated by env vars.

---

## Context Switching

A context is selected in this priority order (evaluated at load time):

```
1. --context <name> CLI flag     (returns ContextNotFound if name absent)
2. current-context field in file
3. "default" (hardcoded fallback if file has no current-context)
```

Implementation in `loadConfigTolerant` (`command.go:64-74`):
```go
if opts.Context != "" {
    overrides = append(overrides, func(cfg *config.Config) error {
        if !cfg.HasContext(opts.Context) {
            return config.ContextNotFound(opts.Context)
        }
        cfg.CurrentContext = opts.Context
        return nil
    })
}
```

To switch permanently: `grafanactl config use-context <name>` writes the
updated `current-context` field back to the file (`command.go:384-405`).

`GetCurrentContext()` (types.go:33):
```go
func (config *Config) GetCurrentContext() *Context {
    return config.Contexts[config.CurrentContext]
}
```

Returns `nil` if `CurrentContext` is empty or not found — callers must check.

---

## From Config to REST Client

Once a context is loaded, it converts to a `NamespacedRESTConfig` which is
passed to the k8s dynamic client. This is the bridge between grafanactl's
config model and Kubernetes client-go:

```
Context.ToRESTConfig(ctx)
  └── NewNamespacedRESTConfig(ctx, context)   [rest.go:19]
        ├── rest.Config {
        │     Host:    GrafanaConfig.Server
        │     APIPath: "/apis"
        │     QPS:     50       (hardcoded — TODO: make configurable)
        │     Burst:   100      (hardcoded)
        │   }
        ├── TLS mapping: grafanactl TLS → rest.TLSClientConfig
        ├── Auth: APIToken → BearerToken  (priority 1)
        │         User/Password → Username/Password  (priority 2)
        └── Namespace resolution (see below)
```

Source: `internal/config/rest.go`

---

## Namespace Semantics: org-id vs stack-id

"Namespace" in grafanactl corresponds to the Kubernetes namespace used for all
API calls to Grafana's K8s-compatible API. The mapping differs for on-prem vs
cloud:

```
On-prem Grafana:        OrgID  → authlib.OrgNamespaceFormatter(OrgID)
                                  e.g., OrgID=1  → "org-1"

Grafana Cloud:          StackID → authlib.CloudNamespaceFormatter(StackID)
                                  e.g., StackID=12345 → "stacks-12345"
```

Namespace resolution order in `NewNamespacedRESTConfig` (`rest.go:57-68`):

```
1. Try DiscoverStackID() via /bootdata HTTP call
   → if success: use discovered stack-id (overrides even explicit org-id)
2. If discovery fails:
   a. OrgID != 0  → use org namespace ("org-N")
   b. OrgID == 0  → use configured stack-id ("stacks-N")
```

This means: if you configure `org-id` but the server is actually Grafana Cloud,
the discovered stack-id takes precedence silently. See `rest.go:59-61`.

---

## Stack ID Auto-Discovery

For Grafana Cloud instances, the stack ID can be automatically discovered from
the `/bootdata` endpoint, avoiding the need to configure it manually.

Flow (`internal/config/stack_id.go`):

```
DiscoverStackID(ctx, GrafanaConfig)
  1. Build URL: server + "/bootdata"  (strips trailing slash, clears query/fragment)
  2. HTTP GET with 5s timeout, respects TLS config
  3. Parse JSON: { "settings": { "namespace": "stacks-12345" } }
  4. authlib.ParseNamespace("stacks-12345") → extracts StackID=12345
  5. Return int64(12345)
```

Validation behavior (`types.go:106-145`):
- `OrgID != 0` → skip discovery entirely (short-circuit, no HTTP call)
- Discovery succeeds, no `StackID` in config → valid (use discovered ID)
- Discovery succeeds, `StackID` in config matches → valid
- Discovery succeeds, `StackID` in config mismatches → `ValidationError` with "mismatched"
- Discovery fails, `StackID` in config set → valid (use configured ID)
- Discovery fails, no `StackID`, no `OrgID` → `ValidationError` with "missing"

---

## The Editor Abstraction (SetValue / UnsetValue)

The `config set` and `config unset` commands use a reflection-based path
traversal to modify config fields without code-generating a setter per field.

```go
// internal/config/editor.go:11-21
func SetValue[V any](input *V, path string, value string) error
func UnsetValue[V any](input *V, path string) error
```

Path format: dot-separated YAML tag names.

Examples:
```bash
grafanactl config set current-context production
grafanactl config set contexts.dev.grafana.server https://grafana-dev.example.com
grafanactl config set contexts.dev.grafana.org-id 1
grafanactl config set contexts.dev.grafana.tls.insecure-skip-verify true

grafanactl config unset contexts.prod          # removes entire context entry
grafanactl config unset contexts.dev.grafana.user
```

Path traversal algorithm (`editor.go:24-157`):
- Splits path on `.`
- At each step: if `reflect.Struct` → match field by yaml tag name
- If `reflect.Map` → use step as map key, auto-create entry if missing
- If `reflect.Ptr` and nil → allocate new value, then recurse
- Leaf kinds handled: `string`, `[]byte` (slice), `bool`, `int64`
- `unset` mode: resets to zero value at the leaf or removes map entry

Important: path traversal uses **YAML tag names** (e.g., `insecure-skip-verify`),
not Go field names (e.g., `Insecure`). The tag lookup is at `editor.go:49`:
```go
yamlName := strings.Split(fieldType.Tag.Get("yaml"), ",")[0]
if yamlName != step { continue }
```

Adding a new config field: add the Go struct field in `types.go` with a `yaml`
tag, an `env` tag (if env var override is desired), and optionally
`datapolicy:"secret"`. The editor, env override, and redactor all work
automatically via reflection — no additional registration needed.

---

## Secret Handling and Redaction

The `config view` command redacts secrets by default unless `--raw` is passed.
Two separate redaction mechanisms are applied:

### 1. Struct-tag redaction (`secrets.Redact`)

Fields marked `datapolicy:"secret"` in the config structs:
- `GrafanaConfig.Password`  (string)
- `GrafanaConfig.APIToken`  (string)
- `TLS.KeyData`             ([]byte)

`secrets.Redact[V any](value *V)` in `internal/secrets/redactor.go`:
- Walks the struct tree via reflection
- When a field with `datapolicy:"secret"` is found, replaces non-zero string
  with `"**REDACTED**"` and non-nil `[]byte` with `[]byte("**REDACTED**")`
- Handles: structs, maps (recurse into values), slices (recurse into elements)
- Empty/nil secret fields are left as-is (not replaced with the redacted string)

### 2. Provider config redaction (`providers.RedactSecrets`)

Provider configs are `map[string]map[string]string` — no struct tags are
available. Instead, each registered `Provider` declares its `ConfigKey` list
with a `Secret bool` field.

`providers.RedactSecrets(providerConfigs, registered)` in `internal/providers/redact.go`:
- Builds a per-provider set of non-secret key names from `Provider.ConfigKeys()`
- For each provider config entry, redacts any key that is:
  - declared as `Secret: true`
  - not declared at all (unknown key → secure by default)
  - belonging to an unregistered provider (all keys redacted)
- Empty values are left as-is

Security model: **secure by default** — undeclared and unknown keys are always
redacted.

### Combined usage in `viewCmd` (`command.go`)

```go
if !opts.Raw {
    if err := secrets.Redact(&cfg); err != nil { ... }
    // Also redact provider configs for the current context
    if ctx := cfg.GetCurrentContext(); ctx != nil {
        providers.RedactSecrets(ctx.Providers, registered)
    }
}
```

---

## Validation

Validation happens in two places:

1. **Tolerant load** (`loadConfigTolerant`): used by `config view`, `config check`,
   `config set`, `config unset`. No validation beyond YAML parsing. Allows the
   user to work with partially-valid configs.

2. **Strict load** (`LoadConfig`): used by `resources` commands. Calls
   `ctx.Validate()` which enforces:
   - `GrafanaConfig` must be non-nil and non-empty
   - `Server` must be non-empty
   - Either `OrgID != 0`, discovery succeeds, or `StackID != 0`

`ValidationError` carries a YAML-path string (e.g., `$.contexts.'production'.grafana`)
which `annotateErrorWithSource` uses with `go-yaml`'s `path.AnnotateSource` to
produce source-highlighted error output pointing to the exact YAML location.

---

## Adding a New Config Field: Step-by-Step

1. **Add the struct field** in `internal/config/types.go`:
   ```go
   // In GrafanaConfig:
   Timeout int64 `env:"GRAFANA_TIMEOUT" json:"timeout,omitempty" yaml:"timeout,omitempty"`
   ```
   - Use `json` and `yaml` tags with the same kebab-case name
   - Add `env:"..."` tag if env var override is desired
   - Add `datapolicy:"secret"` if the value is sensitive

2. **No registration needed** — the editor, env parser, and redactor are all
   reflection-driven and will pick up the new field automatically.

3. **Use the field** in `internal/config/rest.go:NewNamespacedRESTConfig` if it
   affects the REST client (e.g., timeout → `rcfg.Timeout`).

4. **Add validation** in `GrafanaConfig.Validate` if the field has constraints.

5. **Test**: add table-driven test cases in `editor_test.go` for `SetValue`/`UnsetValue`
   and in `types_test.go` for validation scenarios.

---

## Complete Loading Call Chain

```
grafanactl resources get dashboards
  └── resources/command.go → opts.LoadRESTConfig(ctx)
        └── cmd/config/command.go:LoadRESTConfig
              └── LoadConfig(ctx)
                    └── loadConfigTolerant(ctx, validator)
                          ├── config.Load(ctx, source, overrides...)
                          │     ├── source() → resolve file path
                          │     ├── os.ReadFile
                          │     ├── YAMLCodec.Decode → Config{}
                          │     ├── populate ctx.Name for each context
                          │     └── apply overrides in order:
                          │           [0] env.Parse(currentContext.Grafana)
                          │           [1] --context flag override (if set)
                          │           [2] validator: ctx.Validate()
                          │                 └── GrafanaConfig.validateNamespace
                          │                       └── DiscoverStackID (HTTP)
                          └── cfg.GetCurrentContext().ToRESTConfig(ctx)
                                └── NewNamespacedRESTConfig(ctx, context)
                                      ├── build rest.Config (host, TLS, auth)
                                      ├── DiscoverStackID (HTTP, second call)
                                      └── return NamespacedRESTConfig{Config, Namespace}
```

Note: `DiscoverStackID` is called twice — once during validation and once when
building the REST config. This is a known inefficiency (no caching between the
two calls).

---

## Global CLI Options

Separate from the context-based configuration, `internal/config/cli_options.go`
provides a global CLI options mechanism for flags and environment variables that
affect command behavior but are not tied to any specific Grafana context.

```go
// internal/config/cli_options.go
type CLIOptions struct {
    AutoApprove bool `env:"GRAFANACTL_AUTO_APPROVE"`
}

func LoadCLIOptions() (CLIOptions, error)
```

`LoadCLIOptions()` uses `caarlos0/env/v11` (the same library used for
context-scoped env vars) to parse global environment variables into a
`CLIOptions` struct. Unlike context overrides, these options are loaded
independently — they do not read from the config file or affect any context.

**Current usage:** The `delete` command calls `LoadCLIOptions()` in its `RunE`
and, when `AutoApprove` is true (or `--yes`/`-y` is passed), automatically
enables the `--force` flag for non-interactive operation in CI/CD pipelines.

| Env Var | CLI Flag | Effect |
|---------|----------|--------|
| `GRAFANACTL_AUTO_APPROVE` | `--yes` / `-y` | Auto-enables `--force` on delete |

See [design-guide.md](design-guide.md) Section 10 for the full environment
variable reference.

---

## Key Files Reference

| File | Purpose |
|------|---------|
| `internal/config/types.go` | All config struct definitions, `Minify`, `Validate` |
| `internal/config/loader.go` | `Load`, `Write`, `StandardLocation`, `ExplicitConfigFile` |
| `internal/config/editor.go` | `SetValue`, `UnsetValue` — reflection-based path traversal |
| `internal/config/rest.go` | `NewNamespacedRESTConfig` — config → k8s REST client |
| `internal/config/stack_id.go` | `DiscoverStackID` — Grafana Cloud namespace discovery |
| `internal/config/cli_options.go` | `CLIOptions`, `LoadCLIOptions` — global CLI env var options |
| `internal/config/errors.go` | `ValidationError`, `UnmarshalError`, `ContextNotFound` |
| `internal/secrets/redactor.go` | `Redact` — reflection-based secret redaction |
| `internal/providers/provider.go` | `Provider` interface + `ConfigKey` type |
| `internal/providers/redact.go` | `RedactSecrets` — provider config redaction |
| `cmd/grafanactl/config/command.go` | CLI commands + `Options.LoadConfig`/`LoadRESTConfig` |
