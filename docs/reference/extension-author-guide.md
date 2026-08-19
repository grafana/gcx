# Extension author guide — outline

Not written yet. This is the list of things we know the guide needs to cover,
gathered from building the two PoC extensions in
[PR #1211](https://github.com/grafana/gcx/pull/1211). Mechanism:
[ADR-023](../adrs/extensions/001-third-party-extensions-design.md) (still
`proposed`).

## Getting started

- An extension is a program plus a `gcx-extension.yaml`. Any language.
- There is no SDK and nothing to link against. Say so explicitly — authors will look for one.
- Point at [`whoami`](../../examples/extensions/whoami) (six lines of shell), [`azure-datasources`](../../examples/extensions/azure-datasources) (real provisioning flow), and [`profile-explorer`](../../examples/extensions/profile-explorer) (a flamegraph TUI) as the three worked examples.
- The local dev loop: build, `gcx ext install .`, run, repeat.

## Reaching Grafana

- You never get a Grafana or Cloud credential. `GCX_EXT_GCX_BIN` is the gcx binary that dispatched you; every Grafana call goes back through it.
- What you inherit for free: auth, token refresh, keychain access, Cloud proxying, context resolution.
- If you need something gcx has no command for, that is a gap to raise, not to work around.

## The three easy mistakes

- **Not passing the context back.** Forgetting it silently operates on `current-context` instead of the stack the user asked for. Worth calling out as the single easiest way to write a dangerous extension.
- **Not forcing `--output json`.** gcx's default codec is human-facing; outside agent mode you get a table you cannot parse.
- **Ignoring stdout on non-zero exit.** A partial failure (exit 4) still writes a complete result document, and the per-item reason is in it. Show the before/after: `exit status 4` vs `Token missing required scope: grafana-api:delete`.

## Reading gcx's output

- Results on stdout, progress and hints and human errors on stderr — so stdout stays one JSON value.
- Envelopes are not uniform (`datasources list` → `{"datasources": [...]}`, `datasources delete` → bare array). Check the shape of each command you depend on.
- Pin what you have tested with `spec.minGCXVersion`. Flag that these shapes are not yet a versioned contract (see the risk in the findings doc).

## Handling secrets

- Pass secrets to gcx by environment variable and reference them with `{fromEnv: ...}` in the manifest, never inline and never in argv.
- `{fromFile: ...}` as the alternative.
- Worked example: piping a manifest to `gcx datasources create -f -` with the secret set on the subprocess.

## Conventions worth matching

- Exit codes: reuse gcx's taxonomy (0/1/2/3/4/5) so callers do not special-case you.
- `GCX_EXT_AGENT_MODE` (or `GCX_AGENT_MODE`, pending finding 3) — default to JSON when the parent is in agent mode.
- Support `--dry-run` on anything that creates or deletes.
- Handle SIGINT and exit 5 rather than leaving half-created artifacts.
- Use `GCX_EXT_NAME` in usage strings — you may be installed under a name that differs from your binary.
- An interactive extension owns the terminal outright, because gcx forwards the real stdio. It must also decide when *not* to: check `GCX_EXT_AGENT_MODE` and whether stdout is a terminal, and fall back to a structured document. Worked example: [`profile-explorer`](../../examples/extensions/profile-explorer).

## Argument boundary

- gcx's global flags go before `ext`; everything after your name is yours.
- Do not define flags that shadow gcx's globals (`--context`, `--agent`, `--no-color`, `-v`).

## Publishing

- One `platforms` row per OS/arch, each with a URL and a **mandatory** `sha256`. Install fails closed; there is no compile-from-source fallback.
- A `path:` row with `os: "*"` / `arch: "*"` is for local builds only.
- `spec.telemetry.reportUsage: false` opts your name out of gcx's usage telemetry. Only the name is recorded, never arguments.
- Worth including: a GoReleaser-to-manifest recipe, once someone has done it once.

## What you do not get

- No discovery — no index, no `gcx ext search`. Users find you because you told them.
- No agent discovery — you do not appear in `gcx commands` or `gcx help-tree`.
- No gcx styling — tables, colours, error boxes are not available to you.
- No sandbox and no review. Your extension runs with the user's full permissions, and gcx audits nothing it installs.

## Open questions for the guide

- Does it live in this repo, or with the extension mechanism's own docs once there is a public docs page?
- Do we want a `gcx ext scaffold` before or after writing this? It changes how much of the getting-started section is prose.
