# Configuration and Context System

## Overview

gcx uses a context-based multi-environment configuration model directly
inspired by kubectl's kubeconfig. A single YAML file (format `version: 1`)
stores named `stacks:` (Grafana connection + provider config), named `cloud:`
entries (grafana.com auth, shared across contexts), and thin `contexts:` that
bind a stack and (optionally) a cloud entry together with per-context
datasource defaults. One context is "current" at any time, and all commands
operate against it unless overridden.

All code lives under `internal/config/` and `cmd/gcx/config/command.go`.

---

## Data Model

```
Config
├── Source          (runtime-only: path of loaded file)
├── Version         1                 // legacy (pre-versioned) configs are auto-migrated on load
├── CurrentContext  "production"
├── Resources       *ResourcesConfig  // global `gcx resources` settings; union-merged with per-stack
├── Stacks          map[string]*StackConfig
│   ├── "production"
│   │   ├── Slug     "mystack"        // optional grafana.com stack slug (derived from Server if unset)
│   │   ├── Grafana  *GrafanaConfig
│   │   │   ├── Server    "https://grafana.example.com"
│   │   │   ├── User      ""
│   │   │   ├── Password  ""            // datapolicy:"secret"
│   │   │   ├── APIToken  "glsa_..."    // datapolicy:"secret"  (takes precedence over User/Password)
│   │   │   ├── OrgID     0             // on-prem: org namespace
│   │   │   ├── StackID   12345         // cloud: stack namespace
│   │   │   └── TLS       *TLS
│   │   │       ├── Insecure    false
│   │   │       ├── ServerName  ""
│   │   │       ├── CertData    []byte   // datapolicy:"secret" on KeyData
│   │   │       ├── KeyData     []byte
│   │   │       ├── CAData      []byte
│   │   │       └── NextProtos  []string
│   │   ├── Providers  map[string]map[string]string
│   │   │   ├── "slo"       {"url": "...", "token": "..."}   // secret keys REDACTED in config view
│   │   │   └── "oncall"    {"url": "..."}
│   │   └── Resources  *ResourcesConfig   // per-stack; union-merged with the global one
│   └── "staging"
│       └── Grafana  *GrafanaConfig ...
├── Cloud           map[string]*CloudEntry
│   └── "grafana-com"
│       ├── Token                "glc_..."   // datapolicy:"secret" — GCOM access policy token
│       ├── OAuthToken           ""          // datapolicy:"secret" — from `gcx cloud login`
│       ├── OAuthTokenExpiresAt  ""          // RFC3339
│       ├── APIUrl               ""          // optional, default https://grafana.com
│       ├── OAuthUrl             ""          // optional, default https://grafana.com
│       ├── Orgs                 []string    // grafana.com org slugs, populated at login
│       └── Stacks               []string    // CAP stack realm = grafana.com slugs, NOT local stack keys
└── Contexts        map[string]*Context
    ├── "production"
    │   ├── Stack        "production"    // name ref into Stacks (required for Grafana access)
    │   ├── Cloud        "grafana-com"   // name ref into Cloud (optional)
    │   └── Datasources  {}              // map[kind→uid]: default datasource per kind
    └── "staging"
        └── Stack  "staging"
```

`Config.Resolve()` wires each context's resolved view from its name refs after
decode, merge, or mutation: `Context.StackEntry`/`.CloudEntry` plus the
`.Grafana`/`.Providers` pointers shared with the stack entry (mutations through
them are visible on the stack). Dangling refs leave nil pointers, which
`Context.Validate` reports.

Source files:
- `internal/config/types.go` — all struct definitions (`Config`, `StackConfig`, `CloudEntry`, `Context`, `GrafanaConfig`, `TLS`)
- `internal/config/errors.go` — `ValidationError`, `UnmarshalError`, `ContextNotFound`

### Comparison to kubectl kubeconfig

| kubectl kubeconfig | gcx config | Notes |
|--------------------|-------------------|-------|
| `clusters[]`       | `stacks{}` (map) | Grafana connection + auth merged into one stack entry |
| `users[]`          | `cloud{}` (map) | Named grafana.com auth entries, shared across contexts |
| `contexts[]`       | `contexts{}` (map) | Thin bindings: `stack` ref + optional `cloud` ref + datasource defaults |
| `current-context`  | `current-context` | Identical concept |
| `namespace` in context | derived from org-id/stack-id | See Namespace Semantics below |

Unlike kubectl, a stack entry pairs server and Grafana auth (they are genuinely
per-stack), while grafana.com credentials — which typically cover a whole org —
live in reusable cloud entries.

---

## Annotated Config File Example

```yaml
# ~/.config/gcx/config.yaml

version: 1                        # current format; absent on legacy configs
current-context: "production"     # which context is active

stacks:
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
    slug: mystack                 # grafana.com stack slug; derived from server if unset
    grafana:
      server: "https://mystack.grafana.net"
      token: "glsa_yyyy"
      stack-id: 12345             # maps to namespace "stacks-12345" in K8s API calls
                                  # can be omitted if auto-discovery succeeds (see below)
    providers:
      synth:
        sm-token: "..."           # per-provider config lives on the stack

  # Basic auth, on-prem dev
  local:
    grafana:
      server: "http://localhost:3000"
      user: "admin"
      password: "admin"           # REDACTED in `config view` output
      org-id: 1

cloud:
  # Named grafana.com (GCOM) auth entries, shared across contexts
  grafana-com:
    token: "glc_xxxx"             # Cloud Access Policy token
    api-url: https://grafana.com  # optional, default https://grafana.com
    orgs: [myorg]                 # populated at login
    # stacks: [slug1]             # CAP stack realm (grafana.com slugs); absent = whole org(s)

contexts:
  production:
    stack: production             # required for Grafana access
  cloud-staging:
    stack: cloud-staging
    cloud: grafana-com            # optional; without it, cloud commands fail at runtime with a hint
    datasources:
      prometheus: my-prom-uid     # per-kind default datasource UIDs
  local:
    stack: local
```

---

## File Location and Loading Order

### File Location Priority (highest to lowest)

```
1. --config <path>          CLI flag → ExplicitConfigFile(path)
2. $GCX_CONFIG       env var  → StandardLocation() checks this first
3. $XDG_CONFIG_HOME/gcx/config.yaml
4. $HOME/.config/gcx/config.yaml
5. $XDG_CONFIG_DIRS/gcx/config.yaml  (e.g., /etc/xdg/...)
```

Source: `internal/config/loader.go` (`StandardLocation` function) and
`cmd/gcx/config/command.go` (`configSource` method).

Constants defined in `loader.go`:
```go
StandardConfigFolder   = "gcx"
StandardConfigFileName = "config.yaml"
ConfigFileEnvVar       = "GCX_CONFIG"
configFilePermissions  = 0o600   // file is always written with these perms
```

If no config file exists at the standard location, an empty one is created
automatically with a single `default` context:

```go
// loader.go
const defaultEmptyConfigFile = `
version: 1
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

Loading steps (in `Load`):
1. Call `source()` to get the file path
2. `os.ReadFile` the file
3. **Detect legacy format by shape** (`isLegacyConfig`) and auto-migrate if it matches — see [Legacy format migration](#legacy-format-migration). Otherwise YAML-decode with `BytesAsBase64: true` (so `[]byte` fields are stored as base64 in YAML)
4. `Config.Resolve()`: populate names from map keys and wire each context's resolved `StackEntry`/`CloudEntry`/`Grafana`/`Providers` views from its name refs
5. **Resolve keychain sentinels (current context only)**: the keychain store is stashed on `Config.keychainStore`, then `Load` resolves sentinels for **only the current context's stack and cloud entries** — it replaces `keychain:gcx:stack:<name>:<field>` and `keychain:gcx:cloud:<name>:<field>` sentinels with the plaintext value from the OS keychain (via `internal/credentials`). Entries referenced only by non-current contexts keep their raw sentinel strings until resolved lazily via `Config.ResolveContext(name)`, which avoids spending ~15ms per keychain lookup on contexts a command never touches. If an override (e.g. the `--context` flag) switches the current context, `Load` resolves the newly-selected context too. Successfully resolved (owner, field) pairs are tracked on `Config.keychainFields` so `Write` can round-trip back to sentinels. A lookup that returns `ErrNotFound` (the entry is genuinely gone) clears the field and drops the dangling reference; a lookup that fails for any other reason (the keychain is unavailable — locked session, missing DBus) clears the in-memory value but records the pair on `Config.keychainPreserve` so `Write` writes the original sentinel back verbatim (it may be a legacy-format sentinel, so it is never re-derived from the owner). This means a transient keychain outage can never permanently erase a sentinel from the YAML. Under `go test`, the default store is a no-op that returns `ErrUnavailable`, so test binaries never prompt the OS keychain.
6. **Migrate plaintext token-shaped secrets**: any plaintext value in a tracked field (stack entries: `grafana.token`, `grafana.password`, `grafana.oauth-token`, `grafana.oauth-refresh-token`, `providers.synth.sm-token`; cloud entries: `token`, `oauth-token`) that is not already keychain-backed is pushed to the store and marked. If at least one field migrated, `Load` calls `Write` so the on-disk YAML is rewritten with sentinels. When the keychain is unavailable, a one-time warning fires and plaintext stays on disk.
7. Apply each `Override` function in order
8. On `ValidationError`, call `annotateErrorWithSource` to embed a YAML-path-aware source annotation

`Write` runs a single keychain-reconcile pass (`reconcileKeychain`) over every secret field before encoding, so the keychain stays in sync no matter how the config was mutated:
- **Write-through**: any plaintext secret (e.g. one just set by `gcx login` or `gcx config set`) is pushed to the keychain and replaced with a sentinel on disk — newly written secrets never linger as plaintext.
- **Cleanup**: a field that was keychain-backed but is now empty (`gcx config unset`, or an auth-method switch that drops the old credential) has its keychain entry deleted instead of orphaned.
- **Preserve**: a pair recorded in `keychainPreserve` is written back as its sentinel without touching the store.

When the keychain is unavailable, write-through falls back to leaving plaintext on disk with a one-time warning. Secret-less writes skip the keychain entirely (`hasSecretsToReconcile`), so they never probe the OS backend.

---

## Legacy Format Migration

The pre-versioned format (every context carrying `grafana`/`cloud`/`providers`
inline) is detected by shape and auto-migrated on load
(`internal/config/migrate.go`). Conversion: each context becomes a same-named
stack entry (1:1, no dedup — Grafana auth is genuinely per-context); identical
cloud configs collapse into one cloud entry named from the api-url host
(`grafana-com`, `grafana-ops-com`); the legacy `default-*-datasource` fields
fold into the `datasources:` map; the old `cloud.stack` slug becomes the stack
entry's `slug`.

Migration deletes nothing: the new bytes are self-verified (decode-back +
validation invariant) before an atomic rename; a write-once
`<file>.legacy.bak` backup of the sentinelized legacy file is written first
(no backup → no persist); keychain entries are copied to the new
`stack:<name>`/`cloud:<name>` keys, never deleted — the legacy keys are what
keep the backup restorable, and `reconcileKeychain` exempts them from
staleness cleanup. Restoring the backup over the config file fully rolls back.
A read-only config file migrates in memory with a warning on every invocation
(correct for baked-in CI configs).

---

## Environment Variable Overrides

> See also [environment-variables.md](../design/environment-variables.md) for the complete
> environment variable reference (core + provider + planned variables).

Environment variables are applied as an `Override` function during load
(`config.ParseEnvIntoContext`, `internal/config/envparse.go`). They patch the
**current context's resolved view** in-place: `GRAFANA_*` variables write into
the resolved `GrafanaConfig` (shared with the stack entry, in memory only),
while the `GRAFANA_CLOUD_*` auth variables synthesize an **ephemeral cloud
entry** — a detached copy of whatever entry the context references, so env
values never leak into the shared named entry or get persisted by `Write`.
`GRAFANA_CLOUD_STACK` overrides the stack slug without mutating the shared
stack config.

The `env` struct tags on `GrafanaConfig` and `CloudEntry` (`types.go`) declare
the mapping:

| Env Var           | Config Field              | Type    |
|-------------------|---------------------------|---------|
| `GRAFANA_SERVER`  | `GrafanaConfig.Server`    | string  |
| `GRAFANA_USER`    | `GrafanaConfig.User`      | string  |
| `GRAFANA_PASSWORD`| `GrafanaConfig.Password`  | string  |
| `GRAFANA_TOKEN`   | `GrafanaConfig.APIToken`  | string  |
| `GRAFANA_ORG_ID`  | `GrafanaConfig.OrgID`     | int64   |
| `GRAFANA_STACK_ID`| `GrafanaConfig.StackID`   | int64   |
| `GRAFANA_CLOUD_TOKEN` | `CloudEntry.Token` (ephemeral) | string  |
| `GRAFANA_CLOUD_API_URL` | `CloudEntry.APIUrl` (ephemeral) | string  |
| `GRAFANA_CLOUD_OAUTH_URL` | `CloudEntry.OAuthUrl` (ephemeral) | string  |
| `GRAFANA_CLOUD_STACK` | stack slug override (in-memory) | string  |

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

To switch permanently: `gcx config use-context <name>` writes the
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
passed to the k8s dynamic client. This is the bridge between gcx's
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
        ├── TLS mapping: gcx TLS → rest.TLSClientConfig
        ├── Auth: APIToken → BearerToken  (priority 1)
        │         User/Password → Username/Password  (priority 2)
        └── Namespace resolution (see below)
```

Source: `internal/config/rest.go`

---

## Namespace Semantics: org-id vs stack-id

"Namespace" in gcx corresponds to the Kubernetes namespace used for all
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

## Cloud Configuration

Grafana Cloud (GCOM) credentials live in named top-level `cloud:` entries,
referenced by contexts via `Context.Cloud`. Several contexts typically share
one entry — the whole point of the split is not repeating the same org token
per context.

```
Config.Cloud map[string]*CloudEntry
  └── "grafana-com"
      ├── Token       — Cloud Access Policy token for GCOM (secret)
      ├── OAuthToken / OAuthTokenExpiresAt — from `gcx cloud login` (no refresh token; re-login on expiry)
      ├── APIUrl      — GCOM base URL (default: "https://grafana.com")
      ├── OAuthUrl    — OAuth login base URL (default: "https://grafana.com")
      ├── Orgs        — grafana.com org slugs, populated at login
      └── Stacks      — CAP stack realm (grafana.com slugs, NOT local stack keys); absent = whole org(s)
```

The binding is optional: a context without a `cloud:` ref passes validation,
and cloud-dependent operations fail at runtime with a recovery hint
(`missingCloudAuthError` in `internal/providers/configloader.go` — it names the
existing entry when exactly one exists). A dangling ref is a validation error.
`gcx cloud login` creates or updates an entry (named from the API URL host
unless the context already references one) and binds it to the current
context. Entries are used by provider implementations (e.g.
`internal/cloud/client.go`) to discover stack metadata via the Grafana Cloud
OpenAPI (GCOM). `Token` and `OAuthToken` are marked `datapolicy:"secret"` and
redacted in `config view` output unless `--raw` is passed.

The stack slug (previously `cloud.stack`) now lives on the stack entry as
`stacks.<name>.slug`, since it identifies the stack rather than the GCOM
credential.

Example:
```yaml
stacks:
  cloud-prod:
    slug: mystack                    # optional: derived from server if not set
    grafana:
      server: "https://mystack.grafana.net"
      token: "glsa_xxxx"
cloud:
  grafana-com:
    token: "glc_xxxx"                # Cloud Access Policy token
    api-url: "https://grafana.com"   # optional: defaults to https://grafana.com
contexts:
  cloud-prod:
    stack: cloud-prod
    cloud: grafana-com
```

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

Before traversal, `config set`/`unset` rewrite bare paths through
`ResolveContextPath` (`internal/config/path.go`): `grafana.*`, `providers.*`,
and `slug` resolve through the current context's stack (`stacks.<name>.*`);
`datasources.*`, `stack`, and bare `cloud` qualify against the current context
(`contexts.<name>.*`); `cloud.<entry>.<field>` is absolute. Removed legacy
paths get a pointed error naming the new one (`cloud.token` → "use
cloud.<entry>.token"; `default-prometheus-datasource` →
`datasources.prometheus`).

Examples:
```bash
gcx config set current-context production
gcx config set grafana.server https://grafana.example.com   # → stacks.<current stack>.grafana.server
gcx config set stacks.dev.grafana.server https://grafana-dev.example.com
gcx config set stacks.dev.grafana.org-id 1
gcx config set stacks.dev.grafana.tls.insecure-skip-verify true
gcx config set datasources.prometheus my-prom-uid            # → contexts.<current>.datasources.prometheus
gcx config set cloud.grafana-com.token glc_xxxx              # absolute cloud-entry path
gcx config set contexts.dev.stack dev                        # context → stack binding

gcx config unset contexts.prod          # removes entire context entry
gcx config unset stacks.dev.grafana.user
```

Note: editing `grafana.*` through a context edits its *stack*, which other
contexts may share — inherent to the kubeconfig model.

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
- `GrafanaConfig.OAuthToken` / `OAuthRefreshToken` (string)
- `CloudEntry.Token` / `OAuthToken` (string)
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
   - `stack`/`cloud` name refs must resolve to existing entries
   - the referenced stack's `GrafanaConfig` must be non-nil and non-empty
   - `Server` must be non-empty
   - Either `OrgID != 0`, discovery succeeds, or `StackID != 0`

   A missing `cloud` ref is *not* a validation error — cloud-dependent
   operations fail at runtime with a hint instead.

`ValidationError` carries a YAML-path string (e.g., `$.stacks.'production'.grafana`)
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
gcx resources get dashboards
  └── resources/command.go → opts.LoadGrafanaConfig(ctx)
        └── cmd/config/command.go:LoadGrafanaConfig
              └── LoadConfig(ctx)
                    └── loadConfigTolerant(ctx, validator)
                          ├── config.Load(ctx, source, overrides...)
                          │     ├── source() → resolve file path
                          │     ├── os.ReadFile
                          │     ├── legacy-shape check → auto-migrate, or YAMLCodec.Decode → Config{}
                          │     ├── Config.Resolve() — wire stack/cloud name refs per context
                          │     └── apply overrides in order:
                          │           [0] ParseEnvIntoContext(currentContext)
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
    AutoApprove bool `env:"GCX_AUTO_APPROVE"`
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
| `GCX_AUTO_APPROVE` | `--yes` / `-y` | Auto-enables `--force` on delete |

See [environment-variables.md](../design/environment-variables.md) for the full environment
variable reference.

---

## Key Files Reference

| File | Purpose |
|------|---------|
| `internal/config/types.go` | All config struct definitions, `Resolve`, `Minify`, `Validate` |
| `internal/config/loader.go` | `Load`, `Write`, `StandardLocation`, `ExplicitConfigFile` |
| `internal/config/migrate.go` | Legacy-format detection and auto-migration |
| `internal/config/path.go` | `ResolveContextPath` — bare `config set` path grammar |
| `internal/config/envparse.go` | `ParseEnvIntoContext` — env var overrides, ephemeral cloud entry |
| `internal/config/keychain.go` | Keychain sentinel resolution and reconcile (`stack:`/`cloud:` owners) |
| `internal/config/editor.go` | `SetValue`, `UnsetValue` — reflection-based path traversal |
| `internal/config/rest.go` | `NewNamespacedRESTConfig` — config → k8s REST client |
| `internal/config/stack_id.go` | `DiscoverStackID` — Grafana Cloud namespace discovery |
| `internal/config/cli_options.go` | `CLIOptions`, `LoadCLIOptions` — global CLI env var options |
| `internal/config/errors.go` | `ValidationError`, `UnmarshalError`, `ContextNotFound` |
| `internal/secrets/redactor.go` | `Redact` — reflection-based secret redaction |
| `internal/providers/provider.go` | `Provider` interface + `ConfigKey` type |
| `internal/providers/redact.go` | `RedactSecrets` — provider config redaction |
| `cmd/gcx/config/command.go` | CLI commands + `Options.LoadConfig`/`LoadGrafanaConfig` |
