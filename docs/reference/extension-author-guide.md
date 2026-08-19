# Extension author guide

What you need to know to write a gcx extension. Draft, tracking the PoC in
[PR #1211](https://github.com/grafana/gcx/pull/1211) — the mechanism is
[ADR-023](../adrs/extensions/001-third-party-extensions-design.md), which is
still `proposed`.

Worked examples: [`whoami`](../../examples/extensions/whoami) (six lines of
shell) and [`azure-datasources`](../../examples/extensions/azure-datasources)
(a real provisioning flow).

## The shape of an extension

An extension is a program plus a `gcx-extension.yaml`. Any language. gcx runs
the program as a subprocess, passes it everything after its name, and forwards
its exit code.

There is no SDK and nothing to link against. `azure-datasources` is written in
Go with an empty `require` block, on purpose.

## You never get a Grafana credential

gcx sets `GCX_EXT_GCX_BIN` to the path of the gcx binary that dispatched you.
Every Grafana or Cloud call goes back through it:

```sh
"$GCX_EXT_GCX_BIN" datasources list --output json
```

You inherit gcx's auth, token refresh, keychain access, and Cloud proxying for
free. There is no way to get a raw bearer token, and that is deliberate: if you
need something gcx has no command for, that is a gap to raise, not to work
around.

## Always pass the context back

`GCX_EXT_CONTEXT` carries the context the parent invocation resolved, including
one the user set with `gcx --context prod ext <you>`. Passing it on every gcx
call is your job:

```sh
"$GCX_EXT_GCX_BIN" datasources list --output json --context "$GCX_EXT_CONTEXT"
```

Forget it and you silently operate on whatever `current-context` happens to be,
which is a different stack from the one the user asked for. This is the single
easiest way to write a dangerous extension.

## Always ask for JSON, and read stdout even on failure

Two rules that are easy to get wrong:

- **Force `--output json`.** gcx's default codec is human-facing, and in a
  non-agent terminal you will get a table you cannot parse.
- **Decode stdout even when gcx exits non-zero.** A partially-failing command
  (exit 4) still writes a complete result document, and the per-item reason is
  in it. Ignore stdout on failure and the best you can report is
  `exit status 4`; read it and you can report
  `Token missing required scope: grafana-api:delete`.

Progress narration, hints, and human-formatted errors go to **stderr**, so
stdout stays a single JSON value you can hand straight to a parser.

Envelopes are not uniform — `datasources list` returns
`{"datasources": [...]}`, `datasources delete` returns a bare array. Check the
shape of each command you depend on, and pin the gcx versions you have tested
with `spec.minGCXVersion`.

## Pass secrets by environment variable, not on the command line

If you have a secret to write into Grafana, put it in the child's environment
and reference it from the manifest instead of inlining it:

```jsonc
{
  "secure": { "clientSecret": { "fromEnv": "MY_EXT_CLIENT_SECRET" } }
}
```

then pipe that manifest to `gcx datasources create -f -` with
`MY_EXT_CLIENT_SECRET` set on the subprocess. The secret never reaches argv,
a file, or the manifest text. `fromFile` exists too if a file suits you better.

## Match gcx's conventions where it is cheap

You own your output and your exit codes; gcx will not reformat or reinterpret
either. That freedom is worth spending on consistency:

- **Exit codes.** Reuse gcx's taxonomy — 0 success, 1 general, 2 usage,
  3 auth, 4 partial failure, 5 cancelled. A caller reading your code should not
  need to special-case you.
- **Machine-readable output when asked.** `GCX_EXT_AGENT_MODE` is `true` when
  the parent gcx is in agent mode. Default to JSON then, text otherwise.
- **Progress to stderr, results to stdout.** Same split gcx uses.
- **Support `--dry-run`** for anything that creates or deletes. Users and
  agents both reach for it first.
- **Handle SIGINT** and exit 5 rather than leaving half-created artifacts.

`GCX_EXT_NAME` is the name you were invoked under, which is what belongs in
your usage strings — you may be installed under a name that differs from your
binary's.

## Argument boundary

Everything after your name is yours; gcx's own global flags go *before* `ext`:

```sh
gcx --context prod ext my-extension provision --dry-run
#   ^^^^^^^^^^^^^^ gcx's           ^^^^^^^^^^^^^^^^^^^^ yours
```

Do not define flags that shadow gcx's globals (`--context`, `--agent`,
`--no-color`, `-v`); a user who puts them in the wrong place will get
confusing results.

## Publishing

Your manifest's `platforms` table lists one row per OS/arch with a URL and a
**mandatory** `sha256`. Install fails closed on a mismatch and there is no
compile-from-source fallback, so every platform you claim to support needs a
real artifact. See
[`gcx-extension.release.yaml`](../../examples/extensions/azure-datasources/gcx-extension.release.yaml).

While developing, use a `path:` row with `os: "*"` / `arch: "*"` pointing at
your local build, and `gcx ext install .`.

`spec.telemetry.reportUsage: false` opts your extension's name out of gcx's
anonymous usage telemetry. Only the name is ever recorded, never your
arguments.

## What you do not get

- **No discovery.** There is no index and no `gcx ext search`. Users find your
  extension because you told them about it.
- **No agent discovery.** You do not appear in `gcx commands` or
  `gcx help-tree`, so an agent will not find you the way it finds built-in
  commands.
- **No gcx styling.** You cannot render gcx's tables, colours, or error boxes.
- **No sandbox, and no review.** Your extension runs with the user's full
  permissions, and gcx does not audit anything it installs.
