# RFC 031: User-managed Agent Skills

**Status**: proposed; draft prototype, not an accepted feature

Tracking issue: #1745

## Summary

Extend `gcx agent skills` with explicit local imports and metadata discovery. A coding agent can author a user-specific Grafana workflow, save it through gcx, find it for a later task and load only the body and references needed. The same agent performs the workflow's commands and verifies their outcome.

## Motivation and scope

Bundled skills describe common tasks. Repeated procedures may additionally depend on a user's query conventions, resource naming or investigation steps. Today [get](../../internal/skills/get.go) reads embedded content and the [catalog](../../internal/skills/catalog.go) excludes unrelated local skills. Harness-native skills are a viable workaround; this proposal explores whether gcx-readable discovery across harnesses is useful enough to maintain.

Demand is unproven. This comes from a workflow discussion, not telemetry establishing broad adoption. The prototype makes the interface and maintenance cost reviewable. It covers local user-level imports, metadata discovery, body/reference reads, replacement, removal and agent instructions. Remote repositories, marketplaces, automatic learning, project auto-discovery and task execution are outside scope. Grafana credentials and shared resources are not involved in local skill management.

## User experience

An agent writes a directory containing SKILL.md with `name`, `description`, instructions and optional comma-separated `metadata.tags`. References hold detailed query patterns.

```sh
gcx agent skills install ./service-debug --dry-run
gcx agent skills install ./service-debug
gcx agent skills search latency --source custom --tag logs --limit 5 -o json
gcx agent skills get custom/service-debug -o text
gcx agent skills get custom/service-debug references/queries.md -o text
gcx agent skills update custom/service-debug --from ./service-debug
gcx agent skills uninstall custom/service-debug --yes
```

Explicit `custom/` identities preserve released unqualified bundled names. Names may coincide because physical storage and addressing differ. Search returns identity, description, source and tags without bodies. Whitespace-separated literal terms match case-insensitively across those fields; all terms and repeated exact tags must match. Results sort by identity. Default limit is five; zero returns all. The output discloses returned count, total matches and truncation state.

List remains bundled-only by default. An explicit `--source bundled|custom|all` selects metadata inventory; search defaults to all. Untargeted update and the existing install/uninstall --all workflows never include custom content. Bundled retirement behavior remains unchanged.

## Technical design

Wiring stays in [cmd/gcx/skills](../../cmd/gcx/skills); filesystem operations live in [internal/skills](../../internal/skills). Existing `--dir` selects the root, defaulting to `~/.agents`. Custom snapshots live in `<root>/custom-skills/<name>`, separate from harness-scanned `<root>/skills`. No Grafana configuration field is added. An explicit get provides instructions to the current agent without activating every saved workflow.

Import validates the whole source before publishing a target. Names use lowercase kebab syntax, at most 64 characters; front matter, nonempty description and workflow instructions are required. Symlinks and .git directories are rejected. Limits are 256 files, 2 MiB per file and 16 MiB total. An ownership marker distinguishes imports from unrelated directories. Rooted filesystem operations bound path access. Reads reject escaping paths and execute no content.

New imports fail on existing targets. Replacement requires one custom identity, a nonempty --from directory and a matching declared name. Mixed bundled/custom batches are rejected. A staged snapshot replaces the saved directory, attempting rollback if publication fails. Removal requires an explicit custom identity and approval flag or configured auto-approval; dry-run mutates nothing. The authored source directory remains intact.

Per-identity exclusive writer locks prevent simultaneous mutations. Crash recovery is manual in this prototype: interruption can leave a `.lock-<name>`, an `.import-*` staging directory, or a previous snapshot. Inspect those artifacts and verify no writer remains before recovery. Automatic stale-lock cleanup or a recovery command needs a separate design before production use.

Finite commands reuse the output layer: structured result in agent/JSON modes, text views where supported, diagnostics on stderr. Existing invocations preserve bundled output shapes. Search carries the experimental stability annotation. The draft's added flags and identities remain subject to design review before release.

The portable gcx skill links [custom authoring guidance](../../claude-plugin/skills/gcx/references/custom-skills.md): applicability, bounded checks, verification, import, later discovery and maintenance. The agent must assess a saved procedure within the user's task and permissions; reading it does not authorize additional mutations.

## Drawbacks and alternatives

This duplicates part of harness-native skill management and introduces another local store. The benefit is a consistent gcx-readable discovery path; the cost is filesystem ownership, addressing and lifecycle maintenance. A documentation-only convention remains the smaller alternative if demand is low.

Separate storage requires an explicit read, while preventing bundled operations from owning user content and avoiding automatic context loading. Literal metadata search requires no second model invocation but has limited semantic and multilingual recall.

## Unresolved questions

- Is demand sufficient? Resolve through issue feedback and observed reuse of the prototype.
- Should harness-native installation replace separate storage? Compare task completion and discovery costs with both approaches.
- Should sources be tracked for later refresh? This draft requires an explicit replacement directory and retains no source location.
- Are namespace spelling, tag format and mutation flags suitable for release? Maintainer RFC review must settle them.

## Validation

Coverage must exercise the author/import/search/read/update/remove loop, literal filtering and honest truncation, invalid metadata, dry-run non-mutation, path/symlink boundaries, source-copy isolation and preservation of bundled lifecycle behavior. A passing prototype establishes mechanics, not broad adoption or correctness of arbitrary user-authored instructions.
