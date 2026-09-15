# Declarative provider resource registration

**Created**: 2026-07-07
**Status**: accepted
**Supersedes**: none

## Context

[ADR-008](../typed-resource-adapter-compliance/001-typed-resource-adapter-foundation.md)
established `TypedCRUD[T]` and the single `providers.Register()` entry point.
It left providers to construct their own `adapter.Registration` values. SLO
repeated its CRUD wiring in two factories, while OnCall maintained a private
generic registration builder. The unused `TypedRegistration[T]` offered a
third construction path.

Registrations also repeated schema and GVK derivation. Adapters produced by
`TypedCRUD.AsAdapter()` returned a nil schema despite having enough type
information to derive one.

## Decision

Keep `TypedCRUD[T]`, `ResourceIdentity`, and `Provider.TypedRegistrations()`.
Add `adapter.Resource[T]` as a declarative registration entry point and migrate
SLO definitions as its first consumer. Lift OnCall's existing builder into
`adapter.BuildRegistration[T, C]` for clients whose methods do not match the
capability interfaces. Remove the unused `TypedRegistration[T]`.

A resource declaration contains its group, version, kind, typed example,
strip fields, natural key, URL template, and client constructor. It derives
schema, GVK, and singular/plural names. Explicit name overrides cover cases
where the simple plural rules do not match the existing resource name.

`adapter.NewProvider` collects declarations, including resources with different
Go types, and supplies their registrations through the existing provider
interface. `WithCommands` accepts a factory that constructs fresh commands
and flag state for each CLI root. This change does not generate CRUD commands. Providers requiring their own configuration
keys or validation retain a custom provider implementation.

### Client construction and capabilities

A provider supplies a lazy `DepsLoader` that resolves configuration using
`providers.ConfigLoader` and creates the appropriate authenticated HTTP
client. `ClientDeps` carries that client, base URL, and namespace to resource
constructors. Constructors reuse the supplied transport. The loader remains
in the provider because importing `providers` from `adapter` would create an
import cycle.

Clients implement only the operations they support: `Lister[T]`, `Getter[T]`,
`Creator[T]`, `Updater[T]`, `Deleter[T]`, and `Validator[T]`. The single
`newCapabilityCRUD` function in `capability.go` tests those interfaces and
wires the corresponding `TypedCRUD` functions. This is the narrow exception
to the type-erasure guidance in [Pattern 16](../../architecture/patterns.md):
capability assertions stay in the adapter package, never in providers.
Provider clients use compile-time interface assertions to catch signature drift.

Unsupported operations return `errors.ErrUnsupported`. Dry-run create/update
uses `Validator` when available; otherwise it skips the mutation and returns
`ErrDryRunUnverified`. It must not report an unvalidated mutation as successful.

### Metadata and SLO migration

`AsAdapter()` derives its schema from the resource type and descriptor.
Both builders carry examples through registration and adapter construction.
Natural-key and deep-link metadata remain available to the generic resource
pipeline.

SLO's create/update-and-refetch behavior moves into its client methods.
Provider commands and generic resource operations call those same methods.
The existing command tree retains its `NewTypedCRUD` entry point; it and the
declarative registration construct separate adapters. Tests compare their
resource output. This preserves the CLI while removing duplicated mutation
logic, without adding command generation to this PR.

## Consequences and alternatives

- New resource declarations derive common metadata and infer supported verbs.
  Existing providers can migrate independently.
- OnCall retains explicit operation wiring through the shared builder; it is
  not migrated to capability interfaces in this change.
- A single interface requiring every CRUD verb would force read-only clients
  to implement unsupported methods, so separate capability interfaces are used.
- Reflection-based resource tags would spread runtime dispatch beyond the
  capability boundary and are not used.
- Command generation, identity embedding helpers, and migration of additional
  providers are deferred until their consumers establish the requirements.
