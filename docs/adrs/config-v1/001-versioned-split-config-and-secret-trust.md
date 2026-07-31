# ADR-022: Versioned Split Config and Source-Bound Secret Trust

**Created**: 2026-07-21
**Status**: proposed
**Proposes to supersede**: storage decisions in ADR-003; entry-merge decisions in ADR-004; config-schema decisions in the login-consolidation ADR

## Context

The original config stored Grafana connection data, Grafana Cloud credentials,
provider settings, and datasource defaults together under each context. That
made credentials difficult to reuse and encouraged the same Cloud credential
to be copied across contexts.

Separating reusable entries introduces a security boundary. A repository-local
config must not be able to select a predictable keychain item created for a
user config, nor combine a repository-controlled destination with a credential
from another layer. This must remain true during lazy secret resolution,
plaintext-to-keychain migration, writes, deletion, and legacy config migration.

Layered migration introduces a second compatibility boundary. Legacy contexts
were merged field-by-field before use. Independently converting partial layers
and then atomically merging named entries can change the effective connection,
even when every individual conversion is structurally valid.

Finally, config write-back must not flatten system, user, and repository files
or persist environment overrides into a file.

## Decision

### 1. The split schema is explicitly versioned

The current schema is `version: 1` and has four principal sections:

- `stacks`: named Grafana connections, Grafana authentication, provider
  settings, and per-stack resource settings.
- `cloud`: named Grafana Cloud credentials and their API/OAuth endpoints.
- `contexts`: thin bindings to a stack and optional Cloud entry, plus
  per-context datasource defaults.
- `current-context`: the selected binding.

Unversioned legacy files are detected by shape and migrated. A declared
version other than `1`, including a future version, is rejected before secret
resolution, migration, backup creation, or any other side effect. Writers also
refuse to serialize unsupported versions.

### 2. Named credential-bearing entries are atomic across layers

A higher-priority `stacks.<name>` or `cloud.<name>` replaces the lower-priority
entry wholesale. It never inherits fields from that lower entry. Contexts may
still merge field-by-field because they contain references and datasource
defaults, not credentials or destinations.

This prevents a higher layer from attaching its server or API endpoint to a
credential supplied by a lower layer. Shadowing can make a configuration
incomplete, but cannot splice trust domains together.

### 3. Keychain references are source- and destination-bound

A keychain-backed value is trusted only when all of the following match the
configuration that is resolving it:

- the canonical source config file;
- the owner kind and owner name (`stack` or `cloud`);
- the exact secret field;
- the normalized destination associated with that credential.

The binding applies equally to eager and deferred resolution. A sentinel copied
to a repository config, another field, another same-named entry, or another
destination is treated as untrusted and does not retrieve the credential.
Each reference also carries a random generation. Rotating a credential stages a
new generation, atomically and durably replaces the config, and only then
removes the prior generation. The old YAML therefore continues to resolve the
old credential if a write fails or the process stops before replacement. If a
post-commit cleanup fails, gcx restores the old YAML only after every deleted
old generation was restored; otherwise it retains the durable new YAML and new
generations so the surviving file never points at a missing account.

Plaintext migration creates bound entries only for the file that actually owns
the plaintext value and uses the same staged write transaction. Reconciliation
preserves unavailable keychain references, removes entries for secrets
explicitly deleted from their owning file, and never lets a stale resolution
marker override a later mutation. Loaded plaintext also retains its original
destination provenance, so an environment override cannot redirect it merely
because the keychain is unavailable.
Legacy unbound keychain entries are used only by the controlled legacy
migration path; ordinary v1 loading does not treat them as portable authority.

A credential-bearing Cloud entry is destination-self-contained. Missing API
and OAuth endpoints are materialized as one coherent environment before the
credential is resolved; contexts cannot derive a different Cloud destination
by selecting another stack. An entry referenced from incompatible Cloud
environments must be split.

### 4. Layered legacy migration must preserve effective semantics

Before changing any discovered file, the loader performs a side-effect-free
preflight over an exact snapshot of every participating layer. It compares:

1. the effective legacy configuration produced by the old field-level merge;
2. the effective v1 configuration produced by converting each layer and using
   the new merge rules.

If the effective public connection, credential, provider, or context binding
would change, loading stops with an actionable migration error and no file,
backup, or keychain mutation. The user must consolidate or rename the
conflicting partial legacy entries before retrying.

Multi-source legacy loads convert only in memory for that invocation. Persisting
one layer while another layer can still fail would not be transactional, so the
user must migrate each discovered layer explicitly after preflight succeeds.
Single-source migration is serialized by canonical source identity. Its
write-once backup is an exact, mode-0600 copy of the current legacy bytes; an
existing backup authorizes replacement only when it exactly matches those
bytes. Both backup creation and config replacement are durably synced before
old credential generations are removed.

### 5. Writes target raw source files

Write-back operations load and modify only the selected destination file. They
may consult the merged effective configuration to decide what complete entry
must be materialized, but they do not serialize the merged object. Environment
and flag overrides remain in-memory and are never persisted incidentally.

Login persistence (`gcx login` and `gcx cloud login`) uses copy-on-write when
changing a Cloud credential referenced by another context: it creates a
uniquely named Cloud entry and rebinds only the initiating context.
Same-credential login operations may continue to reuse an existing entry. A
literal `gcx config set cloud.<entry>...` edit intentionally changes that named
entry and therefore affects every context that references it.

A literal config edit that changes a credential destination invalidates the
old credential in the same atomic write. This lets the one-field editor change
the destination safely; a later login or token edit supplies the new
credential. A no-op or normalization-equivalent destination edit does not
discard it.

### 6. Cloud credential kinds remain distinct

Cloud Access Policy tokens stay in `token`. OAuth credentials stay in
`oauth-token` together with expiry and granted-scope metadata when supplied by
the issuer. A “keep existing” login preserves the credential kind and its
metadata. OAuth authentication and the stored API/OAuth endpoints must describe
the same environment; custom endpoints are not silently authenticated against
the production issuer.

### 7. Auto-discovered repository config is not credential consent

An automatically discovered `.gcx.yaml` may supply repository-owned
credentials and destinations, but it may not attach credentials supplied by
the process environment, command flags, interactive prompts, or a different
config entry to a destination the repository supplies. Fresh Grafana or Cloud
login credentials are rejected before any validation request, OAuth flow,
prompt acceptance, or write. Selecting the file
with `--config .gcx.yaml` or `GCX_CONFIG` is the explicit opt-in; supplying only
a server or endpoint does not authorize the file because its proxy and TLS
settings would still affect the connection. An unchanged, source-bound
credential already owned by the local file remains usable.

The same rule applies to provider-specific direct-auth endpoints. A provider
must resolve its endpoint and every credential it may send from one config
snapshot. A runtime endpoint override cannot reuse a stored credential; it must
be accompanied by the corresponding runtime credential. Even that pair does
not authorize an auto-discovered repository stack, whose TLS and proxy settings
still affect the transport; explicitly select the file instead. Derived provider
tokens, endpoints, and caches are not written implicitly to an auto-discovered
repository file. Explicitly selecting that file authorizes those operations.

File-backed mTLS client certificates and keys are external credentials. An
auto-discovered repository stack cannot select them; explicit config selection
is required. TLS material used for a credential-bearing connection is captured
during resolution and the captured bytes, rather than a later re-read, are used
to build the transport.

## Consequences

### Positive

- Repository-local configuration cannot use predictable keychain identifiers
  to redirect or overwrite a credential owned by another source file.
- Atomic layering has an explicit migration guard instead of silently changing
  the meaning of partial legacy overlays.
- Provider and login writes cannot flatten layers or capture environment
  variables.
- Shared Cloud credentials remain reusable without making one context's
  re-authentication a global mutation.
- Repository config cannot redirect environment, prompt, mTLS, or cross-entry
  provider credentials to a repository-controlled endpoint.
- Future config versions fail closed rather than being interpreted as v1.

### Negative

- Copying a config containing bound keychain sentinels to another path does not
  copy its credentials; users must authenticate for the new file.
- Some layered legacy configurations require manual consolidation before they
  can migrate.
- Source-aware credential keys and migration preflight add implementation and
  test complexity.

### Neutral

- `--config` still bypasses layered discovery and selects one explicit source.
- Environment overrides still affect the selected context at runtime when the
  selected destination is trusted; auto-discovered repository destinations
  require explicit file selection before they can consume runtime credentials.
- The v1 on-disk sections and literal `gcx config set` paths remain unchanged.
