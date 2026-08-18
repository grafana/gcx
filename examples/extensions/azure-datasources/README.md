# azure-datasources — a gcx extension

Provisions Grafana datasources for Azure Monitor and Azure Data Explorer. It
mints one Azure app registration per datasource, binds it a read-only role at
subscription scope, and registers the datasource in Grafana.

This exists as a worked example of the extension contract in
[ADR-023](../../../docs/adrs/extensions/001-third-party-extensions-design.md):
what an author actually has to write, and what they get from gcx for free.

## Try it

```bash
go build -o gcx-ext-azure-datasources .
gcx ext install .

az login
gcx --context <your-stack> ext azure-datasources provision --dry-run
gcx --context <your-stack> ext azure-datasources provision
gcx --context <your-stack> ext azure-datasources cleanup
```

## What the author writes

- **A program.** Any language. This one is Go with a `go.mod` that has no
  dependencies at all — no gcx SDK, no Grafana client, no Azure SDK.
- **A manifest** (`gcx-extension.yaml`). `gcx-extension.release.yaml` shows the
  published shape: one row per OS/arch with a URL and a mandatory sha256.
- **Its own Azure dependency.** `az` is looked up, checked, and shelled out to
  by the extension. gcx knows nothing about Azure.

## What gcx provides

- **Auth.** The extension never sees a Grafana token. It invokes the binary
  named in `GCX_EXT_GCX_BIN` and reads its JSON. OAuth refresh, keychain access,
  and Cloud proxying all stay inside gcx.
- **Context.** `GCX_EXT_CONTEXT` carries the context the parent invocation
  resolved, including one set with `--context`, so the extension targets the
  same stack the user asked for.
- **Secret handling in the other direction.** The minted Azure client secret is
  passed to gcx by environment variable and referenced from the manifest as
  `{fromEnv: ...}`, so it never appears in argv, in a file, or in the manifest
  text.
- **Agent mode.** `GCX_EXT_AGENT_MODE` tells the extension the caller wants
  machine-readable output, so it makes the same choice gcx did.
- **Exit codes.** The extension's exit code is propagated verbatim.

## Known rough edges

These are the design findings this example was built to surface; see the PoC
report for the full list.

- Each gcx command's JSON envelope has to be discovered by hand
  (`datasources list` returns `{"datasources": [...]}`, `datasources delete`
  returns a bare array). There is no published schema for an author to code
  against.
- A gcx command that partially fails writes its per-item reason to **stdout**
  and exits non-zero, so an extension must decode stdout even on failure and
  must always force `--output json`.
- The extension does not appear in `gcx commands` or `gcx help-tree`, so an
  agent cannot discover it the way it discovers built-in commands.
