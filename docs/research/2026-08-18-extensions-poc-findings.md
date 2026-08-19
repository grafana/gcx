# Extensions PoC: findings

**Created**: 2026-08-18
**Relates to**: [ADR-023](../adrs/extensions/001-third-party-extensions-design.md) (PR #1195), the `gcx setup datasources azure` stack (#1101-#1105)

## What was built

ADR-023's mechanism (`internal/extensions`, `cmd/gcx/ext`) plus two extensions on top
of it: [`azure-datasources`](../../examples/extensions/azure-datasources), a real slice
of the #1101-#1105 stack, and [`whoami`](../../examples/extensions/whoami), the same
contract in six lines of shell. Verified end to end against a live Azure tenant and a
live Grafana stack.

Points an author guide should cover are collected separately in
[extension-author-guide.md](../reference/extension-author-guide.md).

## What held up

**No credential handoff is not a compromise.** The hardest thing onboarding does is
write a secret into a datasource, and the extension does it without ever seeing a
Grafana token - it pipes a manifest to `gcx datasources create -f -` with
`secure: {clientSecret: {fromEnv: ...}}` and sets that variable on the child.

**No SDK is needed.** `azure-datasources` has an empty `require` block; `whoami` is a
shell script.

**gcx's structured errors carry through.** A failed delete surfaced Grafana's own
`Token missing required scope: grafana-api:delete` rather than `exit status 4`, because
gcx puts its result document on stdout and keeps everything else on stderr.

## Friction

**1. Flag parsing cannot stop at the extension's name.** Cobra dispatches an unmatched
name to `ext` fine on its own, but the remaining args still hit `ext`'s flag parser, so
`gcx ext azure-datasources provision --dry-run` dies on `Unknown flag`. `DisableFlagParsing`
swallows gcx's own globals and pflag's unknown-flag whitelist drops the extension's, so
the fix is to insert a `--` after the name before Cobra sees the args. The ADR should
state the rule: gcx flags before `ext`, everything after the name verbatim.

**2. A compiled extension cannot be installed from a local build.** Local sources and
`script:` extensions are already covered by the ADR, but every `platforms` row is a URL
plus a checksum, so a manifest cannot point at a binary next to it. The PoC adds a
`path:` row, valid with `os: "*"` / `arch: "*"` because a local build is for the machine
doing the install.

**3. The context variable is a trap.** An extension that forgets to pass
`GCX_EXT_CONTEXT` back on every gcx call silently operates on `current-context` instead
of the stack the user asked for. gcx has no context environment variable today (only
`GCX_CONFIG`); adding `GCX_CONTEXT` and setting it on the child would make this correct
by construction. Alongside it the extension needs only `GCX_EXT_GCX_BIN`, `GCX_EXT_NAME`
for usage strings, and `GCX_AGENT_MODE` set to the resolved value so it can match gcx's
output shape.

**4. gcx's JSON envelopes are an undocumented public API.** `datasources list` returns
`{"datasources": [...]}` and `datasources delete` returns a bare array, and an author
discovers each by running it. If "shell out to gcx and parse its JSON" is the sanctioned
integration path, those shapes are the extension API surface.

**5. A partial failure writes its reason to stdout and exits non-zero.** An extension
therefore has to decode stdout even when the command failed, and has to force
`--output json` on commands whose output it does not otherwise want.

**6. Extensions are invisible to agents.** `gcx ext` classifies as `raw`, which is the
right answer - gcx does not own that stdout. The residual problem is discovery:
installed extensions do not appear in `gcx commands` or `gcx help-tree` at all, so an
agent that today finds `gcx setup datasources azure` would find nothing.

Not implemented: usage telemetry. `ReportUsage` is recorded per installed extension, so
wiring it up needs `root.recordTelemetryInfo` to learn about extension names.

## Risks to carry

**The JSON envelopes become a public API by accident.** Acceptable for v1, but it
happens silently - the first envelope change that breaks an extension gets found by an
author, not by a test. Worth stating in the ADR's Consequences.

**Internal teams routing product capabilities through the extension door.** Already
raised on line 50 of the ADR and reinforced here: Azure onboarding fits the mechanism
technically and would be a poor thing to ship through it. The mitigation is editorial -
the ADR should say who the mechanism is for.

## Is Azure onboarding the right first use case?

As a design probe, yes: it is the hardest realistic shape, and every finding above came
out of building it. It also confirms the technical fit - the stack imports
`internal/datasources`, `internal/plugins`, `internal/config` and `internal/output`, and
the extension replaces all four with `gcx` subprocess calls, losing only the plugin
pre-flight (there is no `gcx plugins install`).

As a shipping decision, no. Onboarding is the discovery worst case: `gcx setup
datasources azure` is found by someone who just installed gcx, `gcx ext install <url>` is
found by someone who already knows the extension exists.

Suggested framing for the ADR: keep Azure as the boundary case - technically a perfect
extension, still belongs in core because of discovery and support ownership - and pick
one genuinely third-party case alongside it for the tangible v1 contract.
