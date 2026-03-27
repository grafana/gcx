# Adaptive Telemetry Provider: CLI UX and Resource Adapter Design

**Created**: 2026-03-27
**Status**: proposed
**Bead**: grafanactl-experiments-e6h.1
**Supersedes**: none

## Context

gcx has three separate top-level commands for Grafana Cloud's Adaptive
Telemetry features: `adaptive-metrics`, `adaptive-logs`, `adaptive-traces`.
These share an identical auth pattern (Basic auth with `{instanceID}:{apToken}`
against signal-specific instance URLs discovered via GCOM stack lookup) and a
similar resource structure (CRUD resources + read-only recommendations with
apply/dismiss actions).

We are porting these to gcx. Key design tensions:

1. **One provider vs three.** The three signals share auth, config, and
   structural patterns. Three providers would triplicate `ConfigKeys()`,
   `Validate()`, and GCOM lookup code.

2. **Which resources get adapters.** Some resources have full CRUD (exemptions,
   policies) while others are bulk-replace (metrics rules) or read-only +
   action (recommendations, patterns). Adapters enforce K8s envelope output,
   which breaks pipe workflows like `show → apply` where bare JSON is needed.

3. **Verb naming for provider-only resources.** CONSTITUTION.md prohibits
   adapter verbs (`list`, `get`, `create`, `update`, `delete`) on
   provider-only resources to prevent confusion between the two access paths.

## Decision

### One provider, three subareas

We will implement a single `adaptive` provider with `metrics`, `logs`, and
`traces` as subareas under the provider command tree. This follows the
`$AREA $NOUN $VERB` grammar: `gcx adaptive {signal} {resource} {verb}`.

**Rejected:** Three separate providers (`adaptive-metrics`, `adaptive-logs`,
`adaptive-traces`). Would triplicate shared code and fragment the config
namespace into three provider entries.

### Two adapter registrations (full CRUD only)

We will register adapters for the two resources with complete CRUD semantics:

| GVK | Ops |
|-----|-----|
| `adaptive-logs.ext.grafana.app/v1alpha1` `Exemption` | list, get, create, update, delete |
| `adaptive-traces.ext.grafana.app/v1alpha1` `Policy` | list, get, create, update, delete |

These integrate with `gcx resources get/push/pull/delete` and output K8s
envelope manifests in JSON/YAML mode.

**Rejected: Read-only adapters for recommendations/patterns/rules.** Adapter
registration forces K8s envelope output in JSON/YAML. These resources
participate in pipe workflows (`show -ojson | jq | apply`) where bare domain
types are the correct format. Wrapping in envelopes would require every action
command (`apply`, `sync`, `dismiss`) to detect and unwrap envelopes, doubling
the input surface. CONSTITUTION.md's rule that "JSON/YAML output is identical
between both paths" would also force the provider commands to emit envelopes,
breaking the pipe flow.

### `show` verb for provider-only collections

Provider-only resources use `show` (not `list`) per CONSTITUTION.md's
prohibition on adapter verb mimicry for provider-only resources. `show`
outputs bare domain-type JSON/YAML, not K8s envelopes.

### Dropped commands

`adaptive-traces insights` and `adaptive-traces tenants` are dropped. Both
use untyped `any` request/response bodies with no schema, no `list` for
insights, and no CRUD beyond list+create for tenants. These appear to be
internal/admin tooling, not user-facing resource management.

### Auth via CloudRESTConfig (no provider-specific config keys)

`ConfigKeys()` returns nil. Auth uses `LoadCloudConfig()` which provides:
- `cloud.token` as the access policy token
- `StackInfo.HMInstancePromURL/ID`, `HLInstanceURL/ID`, `HTInstanceURL/ID`
  for signal-specific endpoints

HTTP calls use `providers.ExternalHTTPClient()` with Basic auth headers set
per-request. This follows the CONSTITUTION.md requirement that providers
calling APIs outside the Grafana server must not use `rest.HTTPClientFor()`.

## Consequences

### Positive

- **Single provider entry** in config, `gcx providers` output, and help text.
  Users manage one area (`adaptive`) not three.
- **Push/pull works** for exemptions and policies via the resources pipeline.
  `gcx resources pull --kind Exemption` exports manifests; `gcx resources push`
  round-trips them.
- **Pipe workflows preserved** for recommendations/rules. `gcx adaptive
  metrics recommendations show -ojson | jq '.[] | ...'` feeds directly into
  downstream commands without envelope unwrapping.
- **No verb confusion.** `show` on provider-only resources clearly signals
  "this is not a resources-pipeline resource."

### Negative

- **Recommendations invisible to `gcx resources get --all`.** Users must use
  the provider path to discover recommendations. Acceptable because
  recommendations are ephemeral/advisory, not managed resources.
- **`show` for collections is slightly unusual.** Most CLIs use `list`. Users
  may need to discover this via `--help`. Mitigated by clear help text.

### Follow-up

- Implementation spec needed for each subarea (metrics, logs, traces) — use
  `/plan-spec` with this ADR as input.
- Architecture docs (`docs/architecture/`) must be updated after implementation
  to reflect the new provider in the package map and provider registry.
