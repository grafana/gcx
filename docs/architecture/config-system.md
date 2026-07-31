# Configuration and Context System

## Overview

gcx uses a context-based multi-environment configuration model directly
inspired by kubectl's kubeconfig. Layered YAML files (format `version: 1`)
store named `stacks:` (Grafana connection + provider config), named `cloud:`
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
│   │   │   ├── APIToken  "glsa_..."    // datapolicy:"secret"
│   │   │   ├── AuthMethod "token"       // authoritative: oauth, token, basic, or mtls
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
│       ├── OAuthScopes          []          // granted scopes from the OAuth issuer
│       ├── APIUrl               ""          // materialized Cloud API destination
│       └── OAuthUrl             ""          // paired OAuth origin
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
`.Grafana`/`.Providers` pointers initially shared with the stack entry.
`ParseEnvIntoContext` deep-clones the selected context's runtime stack view
before applying process-local overrides. Dangling refs leave nil pointers, which
`Context.Validate` reports.

Source files:
- `internal/config/types.go` — all struct definitions (`Config`, `StackConfig`, `CloudEntry`, `Context`, `GrafanaConfig`, `TLS`)
- `internal/config/errors.go` — `ValidationError`, `UnmarshalError`,
  `ContextNotFound`, `UnsupportedVersionError`

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

One credential per stack entry is the intended v1 model: using two identities
against the same stack (human vs CI, read-only vs admin) means defining two
stack entries. A future version can add credential indirection on stack
entries without breaking v1 files.

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
      auth-method: token           # only this Grafana auth credential is attached
      token: "glsa_xxxx"
      org-id: 1                   # maps to namespace "org-1" in K8s API calls
      tls:
        insecure-skip-verify: false
        ca-data: <base64 PEM>     # custom CA bundle (base64-encoded in file)

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
    api-url: https://grafana.com  # optional production environment anchor

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

### Explicit Selection and Layered Discovery

Configuration has two mutually exclusive loading modes:

1. `--config <path>` selects exactly one explicit file and bypasses layering.
2. Otherwise, `$GCX_CONFIG` selects exactly one explicit file and bypasses
   layering.
3. With neither override, `LoadLayered` loads every existing discovered source
   from lowest to highest priority:

   | Priority | Source |
   |----------|--------|
   | Lowest | System config (`$XDG_CONFIG_DIRS/gcx/config.yaml` or platform equivalent) |
   | Middle | User config (`$HOME/.config/gcx/config.yaml`, then the platform `$XDG_CONFIG_HOME` fallback; first found wins) |
   | Highest | Repository config (`.gcx.yaml` in the current directory) |

Source: `internal/config/loader.go` (`LoadLayered`, `DiscoverSources`, and
`StandardLocation`) and `cmd/gcx/config/command.go`.

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

### Layer Merging

`LoadLayered` merges discovered files in priority order (system → user →
local). Merge semantics differ by section, and the distinction is a security
boundary:

- **`stacks` and `cloud` entries are atomic**: a same-named entry in a higher
  layer replaces the lower layer's entry wholesale, never field-by-field.
  This guarantees a credential and its destination (`server`, `api-url`)
  always come from the same file — a repo-local `.gcx.yaml` cannot graft its
  own destination onto an entry whose token lives in the user config. A
  hostile layer can only shadow an entry (breaking it), not combine with it.
  kubeconfig takes named entries wholesale for the same reason.
- **`contexts` merge field-by-field**: they carry name references and
  datasource defaults, no secrets or destinations, so a local layer can
  overlay a `cloud:` binding or a datasource default onto a user-layer
  context. References can only select whole entries, never mix their fields.
- **global `resources` and `diagnostics`** keep field-level layering
  (a higher layer can narrow dry-run assertions or override a single
  diagnostics field).

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
3. Reject any explicitly declared version other than `1` before migration,
   keychain access, backup creation, or another side effect
4. **Detect legacy format by shape** (`isLegacyConfig`) and auto-migrate if it
   matches — see [Legacy format migration](#legacy-format-migration). Otherwise
   YAML-decode with `BytesAsBase64: true`
5. Bind every stack and Cloud entry to the canonical identity of the source file
6. `Config.Resolve()`: populate names and wire each context's resolved views
7. **Resolve keychain sentinels for the selected context**: only a v2 sentinel
   whose digest matches the source, exact owner/field, and normalized credential
   destination can trigger a keychain lookup. A copied, cross-field, legacy, or
   destination-mismatched sentinel is withheld in memory without a lookup and
   recorded as rejected, so a credential consumer returns an actionable error
   before constructing or sending a request.
   Non-current contexts resolve lazily through `Config.ResolveContext`. A missing
   keychain item withholds the plaintext value in memory and records the missing
   state; the original on-disk sentinel survives unrelated writes so the field
   remains visibly configured and repairable. An unavailable keychain likewise
   preserves the sentinel for an unchanged write. Under `go test`, the default
   store is unavailable, so test binaries never prompt the OS keychain.
8. **Migrate plaintext token-shaped secrets**: plaintext values in tracked stack
   and Cloud fields are staged under a newly generated bound account and the
   file is rewritten with
   `keychain:gcx:v2:<binding-digest>:<random-generation>`. The binding covers
   the canonical source, exact owner kind/name, exact field, and normalized
   destination. An incomplete binding or unavailable keychain leaves the value
   in plaintext with a warning.
9. Apply each `Override` function in order, then lazily resolve a context selected
   by an override
10. On `ValidationError`, annotate the error with YAML source information

`Write` validates the schema version and binds the configuration to its actual
target source before encoding. Its keychain reconcile pass is mutation-aware:

- **Write-through**: a plaintext secret is stored under the exact target binding
  and replaced with its opaque v2 sentinel on disk.
- **Cleanup**: an explicitly cleared field or deleted owner removes only the
  bound account that was loaded from that same source.
- **Preserve**: an unchanged sentinel whose backend was unavailable is written
  back verbatim without touching the store. Explicit set/unset intent overrides
  preservation, so stale load state cannot undo a mutation.
- **Reject/rebind**: a foreign, missing, or destination-mismatched sentinel is
  never used as authority. Its on-disk reference and rejection evidence survive
  unrelated writes, while an explicit set/unset can repair or remove it.

Credential writes are a staged transaction. A new or rotated value is written
under a fresh random generation before the config is replaced. The new config
is written to a temporary file, synced, renamed, and followed by a parent
directory sync. A failure before rename removes the staged generation; a
failure after rename but before confirmed durability preserves both generations
for recovery. The prior generation is removed only after the replacement is
durable, so the old YAML always continues to resolve its old credential until
the new YAML has committed. A later cleanup failure rolls the file back only
when every deleted old generation was restored successfully. If restoration is
uncertain, gcx retains the durable new config and staged generations instead of
creating an old-YAML-to-missing-keychain reference.

When the keychain reports a narrowly classified unavailable backend, a
brand-new credential with no prior keychain reference may remain plaintext on
disk; gcx warns at most once per process. Known locked/unreachable native
backend failures are normalized at write time as unavailable, not just during
the initial read probe. Replacing or deleting an existing generation, replacing
a missing or rejected sentinel, and value-size, policy, cancellation, or
unknown backend failures all fail closed. Silently continuing in those cases
could orphan the only resolvable credential, leave an old credential active, or
downgrade a credential for an unrelated backend error. Secret-less writes skip
the keychain entirely (`hasSecretsToReconcile`), so they never probe the OS
backend.

---

## Legacy Format Migration

The pre-versioned format (every context carrying `grafana`/`cloud`/`providers`
inline) is detected by shape (`internal/config/migrate.go`). Conversion makes
each context a same-named stack entry (1:1, no dedup — Grafana auth is genuinely
per-context); identical Cloud configs collapse into one entry named from the
API URL host (`grafana-com`, `grafana-ops-com`); legacy
`default-*-datasource` fields fold into `datasources:`; and the old
`cloud.stack` slug becomes the stack entry's `slug`.

Before any discovered layer is changed, migration preflights exact snapshots of
all participating sources. It compares the effective legacy field-merged view
with independently converted v1 layers under the new atomic-entry rules. A
semantic change or unsafe mixed-schema overlap fails before any config, backup,
or keychain mutation. Multi-source legacy loads convert only in memory; users
then migrate each layer explicitly, because persisting one layer while another
can still fail would not be transactional. The layered loader emits one
consolidated warning per load with the exact
`gcx config set --file <layer> version 1` migration command and
`gcx config edit <layer>` recovery command for every legacy source. If a layer
also hits an exceptional blocker while producing its in-memory view (for
example, an unavailable credential store), that per-file reason is retained in
the same consolidated warning.

A single-source migration is serialized by canonical source identity. It first
creates a write-once, mode-0600 `<file>.legacy.bak` containing the exact raw
legacy bytes. An existing backup authorizes replacement only if its bytes match
the source exactly. Converted bytes are decode-verified before the config is
atomically replaced, and both backup creation and replacement are durably
synced. The backup is deliberately not scrubbed: if the legacy source contained
plaintext credentials, its exact backup retains them indefinitely at mode
`0600` until the user removes it after gaining rollback confidence. Plaintext
values in the new v1 file and trusted legacy keychain values move to
source/owner/field/destination-bound, generation-addressed accounts. Legacy
accounts are never deleted, so an exact backup containing a legacy sentinel
remains restorable. Predictable legacy sentinels are accepted only by the
controlled migrator for the canonical, securely permissioned user source or a
securely permissioned file deliberately selected through
`--config`/`GCX_CONFIG`. Explicit consent is bound to that file's canonical
identity; generic internal construction of an explicit source grants no
authority. Symlinked home/XDG paths work through canonical identity
comparison. These sentinels are not portable authority in ordinary v1 loading.
A read-only source migrates in memory with a warning on every invocation.

---

## Environment Variable Overrides

> See also [environment-variables.md](../design/environment-variables.md) for the complete
> environment variable reference (core + provider + planned variables).

Environment variables are applied as an `Override` function after context
selection (`config.ParseEnvIntoContext`, `internal/config/envparse.go`). Before
parsing them, gcx deep-clones the **selected context's runtime stack view**
(including Grafana, TLS, and nested provider maps) while retaining its persisted
owner/source identity and credential-rejection evidence. `GRAFANA_*` and later
`GRAFANA_PROVIDER_*` overlays therefore cannot leak into sibling contexts that
reference the same stack, mutate `Config.Stacks`, or get persisted by `Write`.
The `GRAFANA_CLOUD_*` auth variables likewise synthesize an **ephemeral cloud
entry** — a detached copy of whatever entry the context references.
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
| `GRAFANA_PROXY_ENDPOINT` | `GrafanaConfig.ProxyEndpoint` | string |
| `GRAFANA_TLS_CERT_FILE` | `GrafanaConfig.TLS.CertFile` | string |
| `GRAFANA_TLS_KEY_FILE` | `GrafanaConfig.TLS.KeyFile` | string |
| `GRAFANA_TLS_CA_FILE` | `GrafanaConfig.TLS.CAFile` | string |
| `GRAFANA_CLOUD_TOKEN` | `CloudEntry.Token` (ephemeral) | string  |
| `GRAFANA_CLOUD_API_URL` | `CloudEntry.APIUrl` (ephemeral) | string  |
| `GRAFANA_CLOUD_OAUTH_URL` | `CloudEntry.OAuthUrl` (ephemeral) | string  |
| `GRAFANA_CLOUD_STACK` | stack slug override (in-memory) | string  |

Key behavior: env vars override the **selected context** only. They do not
affect other contexts in the file. Write-back paths reload their raw target
source, so the env-mutated runtime object is never persisted implicitly.

Blank or whitespace-only credential environment values are treated as absent:
they do not erase a stored credential and do not count as fresh authorization
to rebind it. This applies to the core Grafana, Cloud, password, and Synthetic
Monitoring credential variables and to registered provider keys declared
`Secret`. Blank non-secret and unknown provider fields retain their ordinary
override semantics.

An auto-discovered repository `.gcx.yaml` is a lower trust boundary for
external credentials. Fresh flag-, prompt-, environment-, or OAuth-derived
Grafana, Cloud, Synthetic Monitoring, and basic-auth credentials are withheld
and recorded as rejected when paired with a destination from that local
source. Credential-consuming loaders surface that state as a pre-network error
instead of silently falling back to anonymous or empty Basic authentication.
To authorize the repository file, select it explicitly with
`--config .gcx.yaml` or `GCX_CONFIG`; a server or endpoint flag alone is
insufficient because local proxy and TLS settings still participate in the
connection. Existing plaintext or source-bound keychain credentials may be
reused only for their unchanged bound destination. Direct-provider routes may
still require explicit selection before accepting repository-controlled
endpoints.

Provider-specific endpoint environment variables follow an additional pairing
rule: changing a direct-auth destination requires the provider's corresponding
credential environment variable in the same invocation. Providers resolve the
endpoint, Grafana auth, Cloud auth, and stack metadata from one loaded snapshot
so separate loads cannot splice trust sources. Implicit provider cache or token
write-back skips auto-discovered repository files.

The `resources` command carries its explicit `--config` selection through the
operation context. Lazy provider adapters consume that immutable selection for
reads, destructive mutations, OAuth refresh persistence, and provider cache
write-back. A zero-value adapter loader therefore cannot rediscover a different
user or repository config midway through one command.

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
        ├── Auth: OAuth + proxy → RefreshTransport  (priority 1)
        │         APIToken → BearerToken             (priority 2)
        │         User/Password → Username/Password  (priority 3)
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

Namespace resolution order in `resolveNamespace` (`stack_id.go`):

```
1. Configured StackID != 0 → use "stacks-N" locally without discovery
2. Otherwise try DiscoverStackID() via the memoized /bootdata call
   → success: use discovered "stacks-N" (including when OrgID is configured)
3. Discovery failure with OrgID != 0 → use "org-N"
4. No usable ID → unresolved cloud namespace
```

Validation deliberately avoids an extra discovery call when `StackID` is
configured; it reports a mismatch only when that server already has a cached
discovery result. With neither ID configured, validation performs discovery and
fails if it cannot determine a stack ID.

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
      ├── OAuthToken  — from `gcx cloud login` (no refresh token; re-login on expiry)
      ├── OAuthTokenExpiresAt — issuer-reported RFC3339 expiry when available
      ├── OAuthScopes — granted scope set returned by the issuer
      ├── APIUrl      — GCOM base URL
      └── OAuthUrl    — OAuth login base URL paired with APIUrl
```

A credential-bearing Cloud entry is destination-self-contained. If exactly one
of `api-url` or `oauth-url` is explicit, it supplies the missing peer. If both
are absent, loading derives the sole Cloud environment represented by the
Grafana servers of referencing contexts. It uses `https://grafana.com` when no
reference identifies another environment; incompatible referenced environments
are rejected and must use separate entries. This prevents a
credential authenticated against one issuer from later being presented to an
unrelated API destination.

That rejection is a raw-repair path, not an automatic mutation. The error names
the owning source, every conflicting Cloud root, and the exact
`gcx config edit <layer>` or `gcx config edit --config <path>` command that
opens the file without loading it. Split the Cloud entry into one entry per
environment and update the affected `contexts.<name>.cloud` bindings; gcx never
guesses which destination should receive the credential.

The binding is optional: a context without a `cloud:` ref passes validation,
and cloud-dependent operations fail at runtime with a recovery hint
(`missingCloudAuthError` in `internal/providers/configloader.go` — it names the
existing entry when exactly one exists). A dangling ref is a validation error.
`gcx cloud login` creates or updates an entry (named from the API URL host
unless the context already references one) and binds it to the selected
context. If that entry is shared and the credential, metadata, or endpoint
changes, login creates a uniquely named copy and rebinds only the initiating
context. Literal `gcx config set cloud.<entry>...` edits do not use this login
copy-on-write behavior: they intentionally change the named entry for every
referencing context. Entries are used by provider implementations (e.g.
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
    api-url: "https://grafana.com"   # optional; anchors the Cloud environment
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
- `StackID` in config → valid without a new discovery call; if a successful
  discovery is already cached for the server, a mismatch produces a
  `ValidationError`
- Discovery fails, no `StackID`, no `OrgID` → `ValidationError` with "missing"

Runtime namespace resolution is slightly broader: with no configured
`StackID`, it may still attempt memoized discovery before falling back to a
configured `OrgID`.

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

Before traversal, `config set`/`unset` validate paths through
`ValidateConfigPath` (`internal/config/path.go`). Paths are LITERAL: they
name the exact location in the file, starting from a top-level section
(`stacks.<name>.`, `cloud.<entry>.`, `contexts.<name>.`, `resources.`,
`current-context`). Nothing is routed against the current context — the path
you type is the path `gcx config view` shows. Bare and removed legacy paths
error with the absolute path spelled out, computed from the current context
so the fix is copy-pasteable (`grafana.server` → "use
stacks.dev.grafana.server"; `cloud.token` → "use cloud.<entry>.token";
`default-prometheus-datasource` → `contexts.dev.datasources.prometheus`).

Examples:
```bash
gcx config set current-context production
gcx config set stacks.dev.grafana.server https://grafana-dev.example.com
gcx config set stacks.dev.grafana.org-id 1
gcx config set stacks.dev.grafana.tls.insecure-skip-verify true
gcx config set contexts.dev.datasources.prometheus my-prom-uid
gcx config set cloud.grafana-com.token glc_xxxx
gcx config set contexts.dev.stack dev                        # context → stack binding

gcx config unset contexts.prod          # removes entire context entry
gcx config unset stacks.dev.grafana.user
```

Editing a named `stacks.<name>.*` entry affects every context that references
that stack. Editing a named `cloud.<entry>.*` entry likewise affects every
context that references it. Config paths are literal; there is no implicit edit
through a context reference.

`config set --config <path>` may initialize a missing explicit file, but only
when the loader proves the source was absent on its initial read. The resulting
config carries an absent-source revision marker, and `Write` installs it without
replacement; an unrelated or later `ENOENT` never authorizes creation. Map keys
containing `.` cannot be represented by this dot-path grammar and must be
changed with `gcx config edit`.

Destination edits invalidate only credentials that could otherwise be sent to
the old destination. Changing `stacks.<name>.grafana.server` or
`proxy-endpoint` clears the stack's Grafana token/password/OAuth credentials;
changing `stacks.<name>.providers.synth.sm-url` clears its Synthetic Monitoring
token; and changing `cloud.<entry>.api-url` or `oauth-url` clears the Cloud CAP
or OAuth credential. A no-op or normalization-equivalent endpoint edit
preserves the credential. Supply a fresh credential with a later login or
explicit token edit.

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
    // Also redact provider configs on every stack in the rendered config.
    for _, stack := range cfg.Stacks {
        if stack != nil && stack.Providers != nil {
            providers.RedactSecrets(stack.Providers, registered)
        }
    }
}
```

---

## Validation

Validation happens in two places:

1. **Tolerant or raw-target load**: `config view` and `config check` use
   `LoadConfigTolerant`; `config set` and `config unset` use `LoadForWrite` to
   load only the selected raw destination layer. These paths parse the schema
   but do not require every context to be operational, so users can repair
   partially valid configurations without flattening layered state.

2. **Strict load** (`LoadConfig`): used by `resources` commands. Calls
   `ctx.Validate()` which enforces:
   - `stack`/`cloud` name refs must resolve to existing entries
   - the referenced stack's `GrafanaConfig` must be non-nil and non-empty
   - `Server` must be non-empty
   - a credential withheld because its source, owner, field, or destination was
     rejected cannot fall through to anonymous or incomplete authentication
   - an explicit `auth-method` must be one of `oauth`, `token`, `basic`, or
     `mtls`; it is authoritative over stale fields for other methods unless a
     non-blank `GRAFANA_TOKEN` selects runtime-only token authentication for
     this invocation (the derived selector is never persisted)
   - the selected method must have complete material: OAuth needs its proxy and
     an access or refresh token, token needs a non-empty service-account token,
     explicit Basic needs user and password, and mTLS needs client certificate
     and private key
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
                          ├── build overrides in order:
                          │     [0] --context selection (if set)
                          │     [1] ParseEnvIntoContext(selectedContext)
                          │     [2] validator: ctx.Validate()
                          │           └── GrafanaConfig.validateNamespace
                          │                 └── DiscoverStackID (HTTP)
                          ├── config.LoadLayered(ctx, explicitFile, overrides...)
                          │     ├── explicit file/GCX_CONFIG, or discover system → user → local
                          │     ├── exact-snapshot version + legacy-migration preflight
                          │     ├── load sources; bind source identities and Cloud destinations
                          │     ├── resolve trusted keychain references and migrate plaintext
                          │     ├── merge atomic stack/Cloud entries + field-merged contexts
                          │     ├── Config.Resolve() — wire stack/cloud name refs per context
                          │     └── apply the ordered overrides above
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
| `internal/config/migrate_preflight.go` | Exact-snapshot layered migration safety checks |
| `internal/config/version.go` | Declared-version validation for reads and writes |
| `internal/config/path.go` | `ValidateConfigPath` — literal `config set` path validation + hints |
| `internal/config/envparse.go` | `ParseEnvIntoContext` — env var overrides, ephemeral cloud entry |
| `internal/config/keychain.go` | Source/owner/field/destination-bound, generation-addressed keychain resolution and reconciliation |
| `internal/config/editor.go` | `SetValue`, `UnsetValue` — reflection-based path traversal |
| `internal/config/rest.go` | `NewNamespacedRESTConfig` — config → k8s REST client |
| `internal/config/stack_id.go` | `DiscoverStackID` — Grafana Cloud namespace discovery |
| `internal/config/cli_options.go` | `CLIOptions`, `LoadCLIOptions` — global CLI env var options |
| `internal/config/errors.go` | `ValidationError`, `UnmarshalError`, `ContextNotFound`, `UnsupportedVersionError` |
| `internal/secrets/redactor.go` | `Redact` — reflection-based secret redaction |
| `internal/providers/provider.go` | `Provider` interface + `ConfigKey` type |
| `internal/providers/redact.go` | `RedactSecrets` — provider config redaction |
| `cmd/gcx/config/command.go` | CLI commands + `Options.LoadConfig`/`LoadGrafanaConfig` |
