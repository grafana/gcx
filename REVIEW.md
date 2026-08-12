# Review instructions

Calibration for automated review of this repository. Report only what a human
would have to act on.

## What Important means here

Reserve 🔴 Important for:

- Violations of `CONSTITUTION.md` or `DESIGN.md`. This includes the agent output
  contract: a command declared `finite` in
  `cmd/gcx/root/testdata/output_classes.json` must emit exactly one JSON value
  on stdout, so a flag that writes to a file and leaves stdout empty is a
  violation, not a nit. It also includes stdout/stderr routing — a status or
  fallback notice written to stdout lands inside a user's redirected output.
- Regressions to a released command surface: a removed or narrowed flag value, a
  changed exit code, or an output shape existing `--json` or `--jq` callers
  depend on. The surface is stable within a major version.
- Behaviour that changes inside a diff described as a refactor or an extraction.
- A credential — token, authorization code, password — that an error message
  echoes back, or that a prompt leaves in the terminal's input queue when the
  flow exits early, where the shell reads it after gcx exits. Where a diff adds
  a control for this (a redaction, a clear-your-terminal notice, a re-prompt),
  check that it fires on every exit path and not only on success.

Naming, structure, duplication, and test-shape findings are 🟡 Nit at most, even
when the argument for them is strong.

## Verification bar

A behaviour claim needs a `file:line` citation in the source, not an inference
from naming or from a doc comment. Where a comment and the code disagree, the
code is the finding and the comment is a second one. Drop anything you could not
establish — there is no author in the loop to answer a speculative finding.

## Do not report

- Anything CI already enforces: `mise run lint`, the test suite,
  `reference-drift`, `validate-skills`, and the conformance suites in
  `cmd/gcx/root/`.
- Generated files under `docs/reference/cli/`, and anything in `vendor/`.
- Missing test coverage in files this PR did not touch.

## Volume

Report at most six 🟡 Nits. If you found more, give the count in the summary
rather than posting them. After the first review of a PR, post 🔴 Important
findings only — a one-line fix should not reach round seven on style.

## Summary shape

Open with a tally — `2 important, 4 nits` — and lead with "no blocking issues"
when there are none. State conclusions, not process: what the code does is
useful, how you checked it is not. No opening praise.

Close with exactly this line, so an author who pushes a fix knows how to ask
again:

> Comment `@claude review` for a fresh review.
