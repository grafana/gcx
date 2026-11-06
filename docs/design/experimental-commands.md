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

## Internal-only commands

An experimental command whose backend is not available outside Grafana Labs is also hidden. Set `Hidden: true` on the top command of its subtree. Help and `gcx commands` omit a hidden command, `gcx commands --include-hidden` lists it, and it still runs by name. Prefer hiding a group to hiding a leaf. Most root consistency tests skip a hidden leaf but still check the children of a hidden group. A hidden leaf keeps its agent output check while it stays listed in `cmd/gcx/root/testdata/output_classes.json`.

## Enforcement

`cmd/gcx/root/experimental_test.go` checks that the short descriptions, long descriptions, and metadata for experimental commands conform to this design.

## What users are told

The guarantee itself is documented for users in [docs/sources/overview.md](../sources/overview.md#experimental-commands).
