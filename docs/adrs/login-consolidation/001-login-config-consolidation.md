# Login & Config Consolidation

**Created**: 2026-04-16
**Accepted**: 2026-04-17
**Status**: accepted
**Supersedes**: none

<!-- Status lifecycle: proposed -> accepted -> deprecated | superseded -->

## Context

Users face 6-8 manual steps before first useful output from gcx: three `config set` calls
to populate server, token, and namespace fields, then `auth login` for OAuth, then
`config use-context`, then `config check` to verify. The boundaries between `gcx auth`,
`gcx config`, and `gcx setup` are unclear — auth is conceptually part of config but lives
as a separate top-level command.

The UX consistency design (`docs/plans/2026-04-14-ux-consistency-design.md`, section D1)
proposes a unified `gcx login` command that collapses the entire onboarding flow into a
single guided interaction.

This ADR decides the internal architecture, command structure, Cloud detection strategy,
auth deprecation path, config schema changes, CI/agent support, and validation approach
for `gcx login`.

### Forces

- **CONSTITUTION**: Config follows kubectl kubeconfig pattern — named contexts,
  env var overrides, same precedence rules.
- **CONSTITUTION**: Strict layer separation — `cmd/` is CLI wiring only.
- **CONSTITUTION**: All HTTP clients via `httputils` (except K8s tier).
- The existing config schema already has `CloudConfig.Token` (env: `GRAFANA_CLOUD_TOKEN`),
  `GrafanaConfig.APIToken` (env: `GRAFANA_TOKEN`), and OAuth fields
  (`OAuthToken`, `OAuthRefreshToken`, `ProxyEndpoint`).
- `internal/auth.Flow` already implements browser-based OAuth PKCE.
- `config.ContextNameFromServerURL()` and `stackSlugFromServerURL()` already handle
  context name derivation and Cloud detection for `*.grafana.net`, `*.grafana-dev.net`,
  and `*.grafana-ops.net`.
- OAuth expansion (#310/#311) is out of scope — wire to existing OAuth flow.

## Decision

We will create a new `internal/login` package with a package-level `Run` function that
orchestrates the full login lifecycle. The `cmd/gcx/login/` command is thin Cobra wiring
that calls `login.Run()`. `gcx auth` is deprecated via Cobra's built-in deprecation
mechanism.

### 1. Package Structure

```
internal/login/
  login.go       # Run(), Options, Result types
  detect.go      # Cloud detection heuristic
  validate.go    # Connectivity validation
  login_test.go  # Table-driven tests
```

```
cmd/gcx/login/
  command.go     # Cobra command, flag parsing, interactive prompts -> login.Options -> login.Run()
```

### 2. Core Types

```go
package login

// Target identifies what kind of Grafana instance the user is connecting to.
type Target int

const (
    TargetUnknown Target = iota // Ambiguous — CLI must prompt or error
    TargetCloud                 // *.grafana.net, *.grafana-dev.net, *.grafana-ops.net
    TargetOnPrem                // localhost, RFC 1918, IPv6 ULA
)

// Options configures a login invocation. All fields have flag equivalents.
type Options struct {
    // Server is the Grafana server URL. Required (prompted if empty).
    Server string

    // ContextName overrides the auto-derived context name.
    // When empty, derived via config.ContextNameFromServerURL().
    ContextName string

    // Target overrides auto-detection. When TargetUnknown, detection runs.
    Target Target

    // GrafanaToken is a pre-supplied SA token (skips auth method prompt).
    GrafanaToken string

    // CloudToken is a pre-supplied Cloud Access Policy token (skips step 2 prompt).
    CloudToken string

    // CloudAPIURL overrides the GCOM base URL (default: "https://grafana.com").
    // Maps to CloudConfig.APIUrl. For internal environments only (grafana-dev.com, etc.).
    CloudAPIURL string

    // UseOAuth forces the OAuth browser flow for step 1.
    UseOAuth bool

    // Yes disables all interactive prompts. When set, missing required values
    // (server URL, auth method for ambiguous targets) cause Run to return a
    // structured error listing what's missing rather than prompting.
    Yes bool

    // ConfigSource controls which config file to write to.
    ConfigSource config.ConfigSourceFunc

    // NewAuthFlow creates an auth.Flow for OAuth. Injected for testing.
    NewAuthFlow func(server string, opts auth.Options) AuthFlow

    // Writer is the output writer for user-facing messages (stderr).
    Writer io.Writer
}

// AuthFlow is the interface satisfied by auth.Flow. Extracted for testability.
type AuthFlow interface {
    Run(ctx context.Context) (*auth.Result, error)
}

// Result captures what login did.
type Result struct {
    ContextName    string
    AuthMethod     string   // "oauth", "token", "basic"
    IsCloud        bool
    HasCloudToken  bool
    GrafanaVersion string
    Capabilities   []string // e.g., "k8s-api", "cloud-api"
}
```

### 3. `login.Run()` Logic

```
login.Run(ctx, *opts) -> Result
|
+-- 1. Resolve server URL
|   +-- From opts.Server (flag/env)
|   +-- Or: return NeedInput error (CLI layer prompts, retries)
|
+-- 2. Derive context name
|   +-- opts.ContextName if set
|   +-- config.ContextNameFromServerURL(server)
|   +-- If context exists and opts has no explicit tokens: re-auth mode
|
+-- 3. Detect target (Cloud vs on-prem vs ambiguous)
|   +-- opts.Target if != Unknown (flag override)
|   +-- Known Cloud domain -> Cloud
|   +-- Known-local (localhost, RFC 1918, ...) -> on-prem
|   +-- Ambiguous -> NeedClarification (CLI prompts "Is this Cloud?")
|
+-- 4. Step 1: Grafana authentication
|   +-- If opts.GrafanaToken set -> use it (SA token path)
|   +-- If opts.UseOAuth or interactive Cloud -> run auth.Flow (OAuth)
|   |   (OAuth is Cloud-only -- internal/auth.ValidateEndpointURL
|   |    restricts callback hosts to *.grafana[-dev|-ops].net + localhost)
|   +-- Otherwise -> return NeedInput (CLI prompts for token)
|
+-- 5. Step 2: Cloud API auth (Cloud targets only)
|   +-- If opts.CloudToken set -> use it
|   +-- If on-prem -> skip
|   +-- Otherwise -> return NeedInput (CLI prompts for CAP token, Enter to skip)
|
+-- 6. Validate connectivity
|   +-- GET /api/health (reachability + basic auth check)
|   +-- discovery.NewDefaultRegistry() (K8s API availability)
|   +-- grafana.GetVersion() (version >= 12 check)
|   +-- If Cloud + CloudToken: GCOM GetStack() (validates CAP token + discovers stack)
|
+-- 7. Save config
|   +-- Load existing config (tolerant)
|   +-- Create/update context with all fields
|   +-- Set auth-method field
|   +-- Set as current context
|   +-- config.Write()
|
+-- 8. Return Result
```

The `NeedInput` / `NeedClarification` pattern keeps `internal/login` pure: it never
prompts directly. The `cmd/` layer catches these sentinel errors, prompts the user,
fills in the option, and retries `Run()`. This satisfies CONSTITUTION's layer separation.

### 4. Cloud Detection Heuristic

Detection triages server URLs into three buckets: confidently-Cloud,
confidently-local, and ambiguous (ask the user).

```go
// DetectTarget classifies a server URL as Cloud, on-prem, or ambiguous.
func DetectTarget(serverURL string) Target {
    parsed, err := url.Parse(serverURL)
    if err != nil || parsed.Hostname() == "" {
        return TargetUnknown
    }

    // Known Grafana Cloud domains.
    if _, ok := stackSlugFromServerURL(serverURL); ok {
        return TargetCloud
    }

    // Obviously local/private: skip the prompt and assume on-prem.
    if isLocalHostname(parsed.Hostname()) {
        return TargetOnPrem
    }

    // Everything else is ambiguous -- custom domains pointing at Cloud
    // look identical to on-prem URLs from the client side.
    return TargetUnknown
}

func isLocalHostname(host string) bool {
    switch host {
    case "localhost", "127.0.0.1", "::1":
        return true
    }
    if strings.HasSuffix(host, ".localhost") {
        return true
    }
    if ip := net.ParseIP(host); ip != nil && ip.IsPrivate() {
        return true // RFC 1918 (10.x, 172.16-31.x, 192.168.x) + IPv6 ULA (fd00::/8)
    }
    return false
}
```

**Bucket 1 -- Confidently Cloud** (`TargetCloud`): `*.grafana.net`,
`*.grafana-dev.net`, `*.grafana-ops.net`. Matches the OAuth endpoint trust list
in `internal/auth/flow.go` exactly. No prompt.

**Bucket 2 -- Confidently local** (`TargetOnPrem`): `localhost`, loopback
addresses, `*.localhost`, RFC 1918 private IPv4, IPv6 unique local addresses
(`fd00::/8`). No prompt. Deliberately excludes `.local` (mDNS) and
`.internal`/`.corp`/`.lan` (company conventions vary) -- those fall through to
the prompt, which is cheaper than guessing wrong.

**Bucket 3 -- Ambiguous** (`TargetUnknown`): everything else. Most common case
is a Grafana Cloud customer with a vanity domain (e.g. `grafana.acme.com`
pointing at a Cloud stack). From the URL alone these are indistinguishable
from on-prem deployments behind public DNS. The CLI prompts:

```
Is this a Grafana Cloud instance?
  > Yes -- prompt for a Cloud Access Policy token next
    No  -- on-prem, skip CAP token
```

**Non-interactive path** when detection returns `TargetUnknown`:
- `--cloud` or `--cloud-token` sets target to `TargetCloud` explicitly.
- `--yes` / agent mode with no such flag: default to `TargetOnPrem`. CI never
  hangs on the prompt; users who need Cloud in automation must pass `--cloud`
  (or a `--cloud-token`, which implies it).

**GCOM URL override** (`--cloud-api-url`): internal environments use a non-default
GCOM endpoint (e.g. `grafana-dev.com`, `grafana-ops.com`). The config already has
`CloudConfig.APIUrl` (env: `GRAFANA_CLOUD_API_URL`, config path: `cloud.api-url`)
which `ResolveGCOMURL()` honours. `gcx login` exposes this as a `--cloud-api-url`
flag so internal devs can set it in a single command rather than a separate
`gcx config set` call. The field is optional and skipped entirely for external users.

### 5. Two-Step Auth Orchestration

**Step 1 -- Grafana auth** authenticates against the Grafana server itself:
- **OAuth path**: Delegates to existing `internal/auth.Flow`. Saves `OAuthToken`,
  `OAuthRefreshToken`, `ProxyEndpoint` to `GrafanaConfig`. Works for Cloud targets.
- **SA token path**: User provides `glsa_` token via `--token` flag or interactive prompt.
  Saved to `GrafanaConfig.APIToken`. Works for both Cloud and on-prem.

**Step 2 -- Cloud API auth** (Cloud targets only):
- User provides a Cloud Access Policy (`glc_`) token via `--cloud-token` or prompt.
- Saved to existing `CloudConfig.Token`.
- **Skippable**: If the user presses Enter without a token, step 2 is skipped. The context
  is created without a CAP token. Commands that need Cloud APIs (SM, adaptive telemetry,
  fleet) will fail with a clear error pointing the user to `gcx login --context X` to
  add the token later.
- **Validation when provided**: GCOM `GetStack(slug)` call validates the token and
  discovers stack metadata (signal URLs, instance IDs). This is the same call that
  `providers.ConfigLoader.LoadCloudConfig()` makes -- login just does it eagerly as a
  validation step.

### 6. `gcx auth` Deprecation Path

> **Superseded:** This approach was replaced with a hard removal (pre-GA). See spec § Negative Constraints NC-008.

```go
// In cmd/gcx/auth/command.go
cmd := &cobra.Command{
    Use:        "auth",
    Deprecated: "use 'gcx login' instead. 'gcx auth' will be removed in a future release",
}
```

Cobra's `Deprecated` field prints a warning on every invocation but still executes
the command. The `auth login` subcommand is rewired to call `login.Run()` internally,
ensuring identical behavior during the transition.

**Timeline**: Deprecation warning for 2 minor versions (approximately 2-3 months given
current release cadence). After that, `gcx auth` is removed and replaced with a hard
error: `"gcx auth" has been removed. Use "gcx login" instead.`

### 7. Config Schema Changes

One new field added to `GrafanaConfig`:

```go
type GrafanaConfig struct {
    // ... existing fields ...

    // AuthMethod records how this context was authenticated.
    // Values: "oauth", "token", "basic". Empty means legacy (inferred from fields).
    AuthMethod string `json:"auth-method,omitempty" yaml:"auth-method,omitempty"`
}
```

**Why**: Enables smart re-auth. When `gcx login --context X` re-authenticates an
existing context, it knows whether to re-run the OAuth browser flow or prompt for a
new SA token. Without this field, re-auth would have to guess from populated fields
(error-prone when a user has both OAuth and SA token set during migration).

**Migration**: The field is `omitempty` -- existing configs without it work unchanged.
`login.Run()` sets it on every login. Code that needs the auth method falls back to
inferring from populated fields when the field is empty (backward-compatible).

No other schema changes. The existing `CloudConfig.Token`, `GrafanaConfig.APIToken`,
and `GrafanaConfig.OAuth*` fields cover all auth storage needs.

### 8. Non-Interactive / CI / Agent Path

**Env var precedence** (unchanged from existing behavior):
```
GRAFANA_SERVER      -> GrafanaConfig.Server
GRAFANA_TOKEN       -> GrafanaConfig.APIToken
GRAFANA_CLOUD_TOKEN -> CloudConfig.Token
```

Env vars are resolved by the config loader at read time, not by login. This means:

- **Agents/CI don't need `gcx login` at all.** Setting env vars is sufficient -- every
  command reads them via config resolution. `gcx login` is for persisting credentials
  to disk.
- `gcx login --yes --server X --token Y --cloud-token Z` is the explicit "persist to
  config file" path for CI that wants durable config (e.g., writing a shared
  `~/.config/gcx/config.yaml` in a Docker image).

**`--yes` behavior**: Suppresses confirmation prompts only. If a required value (server
URL) has no flag/env source, the command errors rather than hanging on a prompt. This
makes `--yes` safe for scripts.

### 9. Connectivity Validation

"Validates connectivity and discovers stack capabilities" means:

| Check | Endpoint | What it proves | Required |
|-------|----------|---------------|----------|
| Reachability | `GET /api/health` | Server is up and auth works | Yes |
| K8s API | `GET /apis` via `discovery.NewDefaultRegistry()` | Grafana 12+ K8s API available | Yes |
| Version | `GET /api/frontend/settings` via `grafana.GetVersion()` | Grafana version >= 12 | Yes |
| Cloud stack | `GCOM GET /api/instances/{slug}` via `cloud.GCOMClient.GetStack()` | CAP token valid, stack metadata discovered | Only if Cloud + CAP token provided |

All checks use existing code paths. Login runs them in sequence (not parallel -- they're
fast and the sequential error messages are clearer for debugging).

On failure at any check, login prints a clear error with the check that failed and
suggestions. The context is NOT saved to config -- the user gets a clean failure, not
a half-configured context.

### 10. Interactive UI

Interactive prompts in `cmd/gcx/login/` use `github.com/charmbracelet/huh`, which is
already a direct dependency and the house pattern -- `cmd/gcx/dev/scaffold.go` uses it
for the same "fill in missing fields" idiom.

**Sequencing**: The CLI layer runs `login.Run()` in a loop, handling each
`NeedInput` / `NeedClarification` sentinel with a `huh` form, then retrying.
In practice this resolves in at most two forms:

1. **Form 1 (pre-detect)** -- collects server URL if not supplied by flag/env.
   This is all that can be asked before `DetectTarget()` runs.
2. **Form 2 (post-detect)** -- collects whatever the detected target requires:
   - If `TargetUnknown`: "Is this Grafana Cloud?" (yes/no)
   - Grafana auth method choice if ambiguous (OAuth for Cloud, SA token for on-prem)
   - CAP token if Cloud (masked input, empty = skip)

Each form is built dynamically: fields are only appended when the corresponding
`opts` field is empty, mirroring `askMissingOpts` in `scaffold.go`. When all
required values come from flags/env, no form is shown at all.

**Agent mode**: `huh` forms require a TTY. When `agent.IsAgentMode()` is true or stdin
is not a TTY, the CLI returns a structured error listing the missing fields instead of
prompting. This keeps `--yes` safe in scripts and gives agents an actionable error.

### Rejected Alternatives

**A: Thin Orchestrator** -- `gcx login` as pure CLI glue with no `internal/` package.
Rejected because auth method selection logic, cloud detection, and validation would live
in `cmd/`, violating CONSTITUTION's layer separation. The logic is testable only via
CLI integration tests, not unit tests.

**C: Config-Centric** -- Extending `internal/config` with login methods. Rejected because
config is already a substantial package (types, loader, editor, merger, env, stack ID
discovery). Adding auth orchestration to it violates single-responsibility and makes the
package harder to reason about. Also proposed hard-removing `gcx auth` without
deprecation, which breaks scripts.

## Consequences

### Positive

- **First-run drops from 6-8 steps to 1.** `gcx login` handles everything.
- **Clean layer separation.** Domain logic in `internal/login`, CLI wiring in `cmd/`.
  Fully unit-testable via injected dependencies (`NewAuthFlow`, `ConfigSource`).
- **No breaking config changes.** `AuthMethod` is additive, `omitempty`, backward-compatible.
- **Existing `gcx auth` users get a graceful deprecation.** Warning for 2 minor versions,
  behavior unchanged during transition.
- **CI/agents are unaffected.** Env vars continue to work. `gcx login` is for persisting
  to disk, not required for operation.

### Negative

- **New package `internal/login/`.** One more package to maintain. However, it's small
  (approximately 200-300 lines) and well-scoped.
- **`AuthMethod` field is redundant with populated auth fields.** Existing code can infer
  the method. The field adds clarity at the cost of minor duplication.
- **Ambiguous URLs trigger an extra prompt.** Users on custom/vanity domains see one
  "Is this Grafana Cloud?" question. Cheap in interactive flow; CI/agent mode defaults
  to on-prem unless `--cloud` or `--cloud-token` is supplied. Future work:
  `/api/frontend/settings` probing to resolve ambiguity without prompting.

### Follow-up Work

- Implement `internal/login` package and `cmd/gcx/login` command.
- Wire `gcx auth login` to call `login.Run()` with deprecation warning.
- Add `--cloud` flag for custom domain override.
- Add `--cloud-api-url` flag mapping to `CloudConfig.APIUrl` (internal environments).
- Update `gcx config check` to show `auth-method` in its output.
- Post-MVP: probe `/api/frontend/settings` for cloud detection of custom domains.
- Post-MVP: when OAuth covers Cloud APIs (#310/#311), step 2 can be removed.

## Decisions added after initial implementation

The following decisions were made while stabilizing the first end-to-end
login flow in PR #529. They extend this ADR.

### Positional CONTEXT_NAME argument

`gcx login [CONTEXT_NAME]` accepts the context as a positional argument. The
global `--context` flag remains as a backward-compatibility fallback; passing
both produces a structured error. Positional form matches kubectl-style
ergonomics and reads naturally for the common case (`gcx login prod`).

### Pre-populate trinary

When `flags.Server == ""`, `cmd/gcx/login` pre-populates `Server` based on
the context argument:

- `gcx login X` + X exists → pre-populate from X (re-auth).
- `gcx login X` + X absent → do not pre-populate (new-context; sentinel
  prompts for server).
- No arg + current context exists → pre-populate from current (re-auth
  current).
- No arg + no current context → first-time setup.

The earlier implementation always pulled `Server` from the current context,
causing `gcx login other` to leak the current context's server into the
"other" context.

### Context-name collision prevention

`StackSlugFromServerURL` appends an env suffix for Grafana-run non-prod
environments: `-dev` for `*.grafana-dev.net`, `-ops` for `*.grafana-ops.net`.
`*.grafana.net` is unchanged. DNS uniqueness guarantees no intra-env slug
collisions; the suffix prevents cross-env clashes.

For non-Cloud URLs, `ContextNameFromServerURL` replaces dots in the hostname
with hyphens (`grafana.example.com` → `grafana-example-com`). IP-safe, no
TLD heuristics.

### Server URL scheme normalization

`login.Run()` prepends `https://` when `opts.Server` has no scheme. Legacy
configs with bare hostnames (from older `gcx config set` usage) previously
produced malformed URLs like `https:///host/api/health` downstream.
Defaulting to `https://` at entry is safer than `http://`.

### StagedContext cache carrier

`login.Options.StagedContext *config.Context` carries partially-resolved
state across sentinel retries. The CLI allocates `&config.Context{}` once
before the retry loop; `resolveGrafanaAuth` writes `StagedContext.Grafana`
on success and short-circuits when it is already set. This prevents the
OAuth browser from re-launching when the retry loop fires for a later
sentinel (e.g., `ErrNeedInput{cloud-token}`). Cache survives all errors
within a single invocation.

### Server-override confirmation

`persistContext` emits `ErrNeedClarification{Field:"allow-override"}` when
the existing context's server differs from the incoming server. The CLI
shows a `huh.Confirm` dialog; `--yes` bypasses. Non-interactive callers
without `--yes` receive a structured error (`fail.DetailedError`). This
closes the silent-overwrite hole where `gcx login existing-name
--server=<new>` would silently replace the server and tokens of the
existing context.

### Mode header

`cmd/gcx/login` prints a one- or two-line header before the retry loop:

- Re-auth: `Refreshing context "X" (server: …)`
- New-context: `Creating new context "X"` plus `Existing contexts: a, b, c`
- First-time setup: `First-time setup: no existing context configured.`

The existing-contexts list doubles as typo defense.
