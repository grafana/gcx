# Extensions PoC: findings from building the Azure onboarding as an extension

**Created**: 2026-08-18
**Relates to**: [ADR-023](../adrs/extensions/001-third-party-extensions-design.md) (PR #1195), the `gcx setup datasources azure` stack (#1101-#1105)

## What was built

A working implementation of ADR-023's mechanism (`internal/extensions`, `cmd/gcx/ext`)
and two extensions that run on it:

- `examples/extensions/azure-datasources` - a representative slice of the #1101-#1105
  stack, rebuilt as a third-party extension. Discovers Azure subscriptions and ADX
  clusters through `az`, mints an app registration + client secret + subscription-scoped
  role assignment per datasource, registers the datasource in Grafana, health-checks it,
  and can clean up everything it created. Zero dependencies in its `go.mod`.
- `examples/extensions/whoami` - the same contract in six lines of shell.

Verified live against a real Azure tenant and a real Grafana stack: discovery, dry-run,
datasource creation with a real secret, gcx error surfacing, cleanup, and exit-code
propagation.

The author-facing conclusions from all of this are collected in
[docs/reference/extension-author-guide.md](../reference/extension-author-guide.md).

## Strengths that held up

**"No credential handoff" is not a compromise.** The hardest thing an onboarding tool
does is write a secret into a datasource, and the extension does it without ever seeing
a Grafana token: it pipes a manifest to `gcx datasources create -f -` with
`secure: {clientSecret: {fromEnv: GCX_EXT_AZURE_CLIENT_SECRET}}` and sets that variable
on the child process. The secret never appears in argv, in a file, or in the manifest
text. Nothing in the Azure engine needed a raw bearer token.

**No SDK is genuinely needed.** `azure-datasources` has an empty `require` block.
`whoami` is a shell script. The ADR's decision to ship no SDK in v1 is supported.

**gcx's structured errors carry through cleanly.** When `datasources delete` failed with
a missing OAuth scope, the extension surfaced Grafana's own reason
(`Token missing required scope: grafana-api:delete`) rather than `exit status 4`,
because gcx emits its result document as JSON on stdout and keeps hints and progress
on stderr. A naive `json.Unmarshal(stdout)` works.

**Exit codes propagate verbatim.** `gcx ext <name>` returns
`gcxerrors.NewAlreadyReportedError(code)`, so gcx adds no second error message on top
of one the extension already reported.

## Friction found

### 1. Cobra dispatches to an unmatched name fine; it cannot stop parsing flags there

Cobra's own fallback already works: `Find` returns the deepest matching command plus the
remaining arguments, so `gcx ext whoami` runs `ext`'s `RunE` with `whoami` in `args`
without any help. Verified by disabling the rewrite and running it.

What Cobra has no hook for is the *flag* half. The remaining arguments still go through
`ext`'s flag parser, so `gcx ext azure-datasources provision --dry-run` fails with
`Unknown flag: --dry-run`. Both obvious fixes are silently wrong:

- `DisableFlagParsing` on `ext` swallows gcx's own global flags - `--context prod` ends
  up in the extension's argv instead of being applied to gcx.
- pflag's `FParseErrWhitelist.UnknownFlags` does the opposite: `parseLongArg` never
  appends an unrecognised flag to `f.args`, so the extension's own flags are dropped
  rather than passed through.

The working answer is to rewrite the argument list before Cobra sees it, inserting a
`--` after the extension's name (`cmd/gcx/root/extargs.go`). The ADR should state the
resulting rule explicitly: **gcx's global flags go before `ext`; everything after the
extension's name is passed verbatim.**

### 2. A compiled extension cannot be installed from a local build

Narrower than first written: ADR-023 does accept a local path as an install *source*,
and its `script:` field gives an interpreted extension a complete local story. The gap
is only for compiled extensions - every `platforms` row is described as a URL plus a
checksum, so there is no way for a manifest to point at a binary sitting next to it, and
an author cannot install and test their own build before publishing it.

The PoC adds a `path:` field, valid with `os: "*"` / `arch: "*"` because a local build is
for the machine doing the install. Some equivalent needs to be in the ADR -
`gh extension install .` exists for exactly this reason.

### 3. `GCX_EXT_GCX_BIN` alone is not enough, and the context variable is a trap

The PoC also sets `GCX_EXT_CONTEXT`, `GCX_EXT_AGENT_MODE`, `GCX_EXT_NAME` and
`GCX_EXT_VERSION`. Agent mode matters because an extension should make the same
structured-output choice gcx made. Context matters more, and it is a correctness hazard:

```
gcx --context ops ext whoami     # a bare `gcx` call inside the extension
                                 # returns "dafyddt-token", not "ops"
```

Nothing enforces that an author reads `GCX_EXT_CONTEXT` and passes it back on every
call. Forgetting it silently operates on the wrong stack.

**Recommendation:** gcx has no context environment variable today (only `GCX_CONFIG`).
Add `GCX_CONTEXT` as a first-class override and set it on the extension subprocess.
Then a bare `gcx` call from an extension inherits the right stack by construction, and
the advisory variable becomes a convenience rather than a trap.

The PoC's four variables should not all survive. Measured against what the extensions
actually use:

| Variable | Verdict |
|---|---|
| `GCX_EXT_GCX_BIN` | Keep. Required, and already in the ADR. |
| `GCX_EXT_NAME` | Keep. An extension can be installed under a name that differs from its binary, so this is the only way it can render correct usage strings. |
| `GCX_EXT_CONTEXT` | Replace with `GCX_CONTEXT` on gcx itself, per the recommendation above. Shipping both would be two ways to say the same thing. |
| `GCX_EXT_AGENT_MODE` | Replace by setting `GCX_AGENT_MODE` on the child. gcx already documents that variable and the subprocess already inherits it; the only case the new one covers is the `--agent` *flag*, which has no environment representation. Worse, today the child sees a contradictory pair under `gcx --agent ext ...`: `GCX_AGENT_MODE=false` next to `GCX_EXT_AGENT_MODE=true`. |
| `GCX_EXT_VERSION` | Drop. Neither example reads it, and `spec.minGCXVersion` already gates version compatibility at install time. |

That leaves two extension-specific variables and two reuses of gcx's own, which is a
smaller contract to commit to in v1.

### 4. gcx's JSON output shapes are an undocumented public API

`gcx datasources list --output json` returns `{"datasources": [...]}`.
`gcx datasources delete --output json` returns a bare array. The author discovers each
envelope by running the command and reading what comes back.

If "shell out to gcx and parse its JSON" is the sanctioned integration path, those
shapes are the extension API surface, and the ADR is silent on whether they are stable
or versioned. This is the largest unaddressed commitment in the design.

### 5. A partially-failing command puts its reason on stdout and exits non-zero

`gcx datasources delete` exits 4 with an empty stderr and the per-item reason in its
stdout document. An extension therefore has to decode stdout *even when the command
failed*, and has to force `--output json` on commands whose output it does not
otherwise want. Worth a paragraph of author guidance.

### 6. The agent-parity gap is real, and gcx's own tests surface it

`TestConsistency_AllLeafCommandsHaveOutputClass` refuses to let a new leaf command exist
without declaring its output protocol class - and none of the eight classes honestly
describes "a third party owns this stdout". The PoC classifies `gcx ext` as `raw`, the
byte-passthrough escape hatch.

`raw` is the right answer and is accepted as such - the class already means "bytes gcx
does not own", which is exactly true here.

The residual problem is discovery, not classification: extensions do not appear in
`gcx commands` or `gcx help-tree` at all, so an agent cannot find them the way it finds
built-in commands. That is the review comment on line 46 of the ADR, and it stands. For
the Azure case specifically: an agent that today finds `gcx setup datasources azure` in
the command catalog would find nothing at all.

### 7. Not implemented

Usage telemetry. The dispatch path records `ReportUsage` per installed extension, so
the ADR's design is implementable, but wiring it needs `root.recordTelemetryInfo` to
learn about extension names. Deferred deliberately - it is orthogonal to the question
this PoC was built to answer.

## Risks to carry, not to solve now

**gcx's JSON output shapes become a public API by accident.** Accepted for v1: the
alternative is a schema commitment nobody is ready to make. The risk is that it happens
silently - extensions pin behaviour that was never promised, and the first envelope
change that breaks one is discovered by an author, not by a test. Worth stating in the
ADR's Consequences so the decision is on the record rather than implied, and worth
revisiting if the "shell out to gcx" pattern gets real adoption.

**Internal teams routing product capabilities through the extension door.** Already
raised on line 50 of the ADR and reinforced by this PoC: the Azure onboarding fits the
mechanism technically and would be a poor thing to ship through it. The mitigation is
editorial rather than technical - the ADR should say who the mechanism is *for*.

## Is the Azure onboarding the right first use case?

**As a design probe: yes, and it earned its keep.** It is the hardest realistic shape -
an external CLI dependency, vendor-specific IAM, credential minting, and a
secret-carrying write into Grafana. Every one of the findings above came out of building
it, and the load-bearing claim in the ADR (no credential handoff) was tested at its
weakest point rather than on a read-only toy.

It also confirms the technical fit: the extension needs nothing from gcx's internals.
The #1101-#1105 stack imports `internal/datasources`, `internal/plugins`,
`internal/config` and `internal/output`; the extension replaces all four with `gcx`
subprocess calls and loses nothing except the plugin pre-flight (there is no
`gcx plugins install` command - the one real capability gap).

**As a shipping decision: no, and for a reason that is itself a finding.** Onboarding is
the discovery worst case. `gcx setup datasources azure` is found by a user who has just
installed gcx; `gcx ext install <url> && gcx ext azure-datasources provision` is found by
a user who already knows the extension exists. ADR-023 deliberately ships no discovery
index, which is fine for a partner tool and wrong for a first-run path. Azure onboarding
is also built by an internal team shipping a stable product capability - exactly the
drift risk raised on line 50 of the ADR.

**Suggested use in the ADR.** Keep the Azure onboarding as the *boundary* case: the
worked example that shows why a capability with an external toolchain dependency is
technically a perfect extension and still belongs in core, because of discovery and
support ownership. Then pick one genuinely third-party case - a partner integration or a
customer-specific migration tool - to make the v1 contract tangible, as the review
comment asks. The two together draw the line better than either alone.
