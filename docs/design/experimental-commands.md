# Experimental Commands

Experimental commands are commands that are not yet stable, or interact with features in Grafana Cloud that are not yet Generally Available. For more information on what that means, see [Release life cycle for Grafana Labs](https://grafana.com/docs/release-life-cycle/).

Experimental commands are exempt from the compatibility promise in [CONSTITUTION.md](../../CONSTITUTION.md#cli-grammar)

## Command docs

The short description for experimental commands should begin with `[experimental]`.

Every command in an experimental subtree is marked, including its children: help and `gcx commands --flat` show a command's short description without its ancestors'.

The long description for an experimental command should begin with:

> This command is experimental. It may be removed, or its subcommands, flags and responses may change without following the normal semantic versioning conventions.

## Agent metadata

Every experimental command carries the `agent.stability` annotation `agent.StabilityExperimental`, including each command inside an experimental subtree.

## Enforcement

`cmd/gcx/root/experimental_test.go` checks that the short descriptions, long descriptions, and metadata for experimental commands conform to this design.

## What users are told

The guarantee itself is documented for users in [docs/sources/overview.md](../sources/overview.md#experimental-commands).
