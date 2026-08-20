# Experimental Commands

> How experimental commands should be advertised

Experimental commands are commands that are not yet stable, or interact with features in Grafana Cloud that are not yet Generally Available. For more information on what that means, see [Release life cycle for Grafana Labs](https://grafana.com/docs/release-life-cycle/).

Marking a command experimental is what buys the exemption from the compatibility promise in [CONSTITUTION.md](../../CONSTITUTION.md#cli-grammar): an unmarked command cannot be changed or removed within a major version.

## Command docs

The short description for experimental commands should begin with `[experimental]`.

If an entire command subtree is experimental, only the top-level command should be marked as experimental in the short description.

The long description for an experimental command should begin with:

> This command is experimental. It may be removed, or its subcommands, flags and responses may change without following the normal semantic versioning conventions.

## Agent metadata

Every experimental command carries the `agent.stability` annotation
(`agent.StabilityExperimental`), including each command inside an experimental
subtree. Agents read one command's metadata at a time and do not walk ancestors,
so the annotation is repeated where the short description is not.

## Enforcement

`cmd/gcx/root/experimental_test.go` checks the three surfaces agree: the
annotation, the `[experimental]` prefix on the short description, and the
preamble on the long description. Flag help uses the same `[experimental]`
prefix by convention, but is not enforced.

## What users are told

The guarantee itself is documented for users in
[docs/sources/overview.md](../sources/overview.md#experimental-commands).
