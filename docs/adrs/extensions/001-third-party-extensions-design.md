# Third-party extensions

**Created**: 2026-08-14
**Status**: proposed
**Supersedes**: none

## Context

gcx has no way for third parties to add their own subcommands today. The CLI tree is built entirely at compile time (`cmd/gcx/root/command.go`), and providers self-register through a single `init()` call each.

The goal is a third-party extension mechanism with four high level goals: easy to author, must run on every OS/arch gcx itself ships for (`linux/darwin/windows` × `amd64/arm64`), must not force extensions to be hosted in one particular place, and must not require a second binary to find or manage them (thinking about [Krew](https://krew.sigs.k8s.io/)).

Three cases make the v1 contract concrete. Two are the ones this mechanism is *for*: a partner shipping `gcx ext <vendor>` that maps their product's resources into Grafana, and a customer-internal migration tool that reads a legacy monitoring config and pushes the resulting dashboards through gcx. Neither wants gcx's review, neither should wait on a gcx release, and neither belongs in this repository.

The third is a boundary case, and it is the more useful one. `gcx setup datasources azure` fits the mechanism perfectly on the technical criteria — it depends on an external toolchain (`az`), it encodes one vendor's IAM model, and it needs nothing from gcx's internals — and it still belongs in core, because onboarding is the discovery worst case and because a Grafana team owns its support. A capability being *implementable* as an extension is not the test. It exists in core as the #1101-#1105 stack and as an extension in [PR #1211](https://github.com/grafana/gcx/pull/1211), which is where the changes below came from.

Full comparative research against five other CLI extension ecosystems (gh, kubectl/krew, cargo, Docker CLI plugins, Helm) is recorded in [docs/research/2026-08-14-extensions-design.md](../../research/2026-08-14-extensions-design.md). This ADR states the decision that research converged on.

## Decision

Add a new top-level tooling area, `ext` (package `cmd/gcx/ext`, `$AREA $VERB` shape, alongside `dev`/`config`/`agent`):

```
gcx ext install <source>
gcx ext list
gcx ext uninstall <name>
gcx ext update [<name>] [--all]
gcx ext <name> [args...]       # run an installed extension
```

Key points of the design:

- **No PATH scanning.** Extensions live in `~/.config/gcx/extensions/<name>/<version>/`, tracked in a single `index.json` gcx reads and writes — the same folder-resolution convention already used for config (`internal/config/loader.go`).
- **Manifest-driven, not name-convention-driven.** Each extension ships a `gcx-extension.yaml` (matching gcx's own `apiVersion`/`kind`/`metadata` shape) declaring version, a `minGCXVersion` constraint, and a `platforms` table listing a URL + sha256 + binary name per OS/arch — resolved declaratively, the same way krew's plugin manifest works, with no download logic the author has to write and no "compile from source" fallback if no row matches. A `script` field lets a non-compiled extension (bash, Python, anything) skip the platform table entirely. A compiled extension needs the equivalent while it is being written, so a row may carry `path` (a binary relative to the manifest) instead of `url`, valid with `os: "*"` / `arch: "*"` — a local build is for the machine doing the install, so there is nothing to select between.
- **Any hosting location.** `gcx ext install <source>` accepts a local path, a direct URL to the manifest, or any git URL — resolved by shelling out to the user's own `git`, never a GitHub API call. No central index is required for an extension to be installable.
- **`update` re-resolves the install source, it does not discover versions.** `gcx ext update` reinstalls from the source recorded at install time. A git source and a manifest URL at a stable path both pick up new versions; a URL pinned to a release tag never will. Authors publish at a floating path, or their users have to reinstall to upgrade. gcx does not crawl for newer versions, and there is no index that could tell it about one.
- **No separate `run` verb.** `ext` registers `install`/`list`/`uninstall`/`update` as real subcommands; anything else in the first argument position is treated as an extension name and looked up in `index.json`. This is the same "known verbs win, anything else is a name" pattern git uses at its own top level. The boundary that follows is part of the CLI grammar and must be documented as such: **gcx's own global flags go before `ext`, and every argument after the extension's name is passed to it verbatim** (`gcx --context prod ext my-ext --dry-run`). Extensions must therefore not define flags that shadow gcx's globals.
- **No credential handoff.** Extensions never receive a Grafana or Cloud token directly, in any form. An extension that needs Grafana/Cloud data shells out to the invoking `gcx` binary and consumes its JSON output (`gcx api`, `gcx resources get`, any provider command with `--output json`), inheriting whatever auth and refresh logic gcx already handles. This mirrors how gh extensions commonly call `gh api` rather than handling GitHub auth themselves.

  Four environment variables carry everything the subprocess needs. `GCX_EXT_GCX_BIN` is the path to the invoking binary (the same idea as Docker's `DOCKER_CLI_PLUGIN_ORIGINAL_CLI_COMMAND`) and `GCX_EXT_NAME` is the name it was invoked under, which can differ from its binary's. The other two are gcx's own variables, set to the values this invocation resolved rather than new ones invented for extensions: `GCX_AGENT_MODE`, so an extension makes the same structured-output choice gcx made, and `GCX_CONTEXT`.

  `GCX_CONTEXT` does not exist yet and adding it is a prerequisite of this decision, not a detail of it. Without it, a `gcx` call from inside an extension resolves `current-context` and silently operates on a different stack from the one `gcx --context prod ext ...` named — a wrong-stack write, not an inconvenience. Passing a context back on every call cannot be left to author discipline.

  Two rules follow for anyone consuming gcx this way, and belong in author-facing docs: always pass `--output json`, because the default codec is human-facing; and decode stdout even when gcx exits non-zero, because a partial failure (exit 4) still writes a complete result document and the per-item reason is inside it.
- **No sandboxing.** The extension subprocess inherits the parent's full environment, the same posture every surveyed tool ships. Checksum verification is mandatory and fails closed for any non-local source; there is no signature/provenance check beyond the checksum.
- **No scaffolding tool, and no SDK, in v1, for any language.** The manifest format already works for any language for free (a `platforms` entry just points at a binary). An author hand-writes their own manifest and program.
- **Usage telemetry via the existing pipeline, extension name included by design.** Extension dispatch hooks into gcx's existing anonymous telemetry (`internal/telemetry`) rather than a separate one, inheriting its opt-out (`telemetry: disabled`/`GCX_TELEMETRY`) for free. Only `gcx ext <name>` — the command and the extension's name — is ever recorded; arguments after the name are never recorded, for the same reason flag values never are today. `<name>` is checked against the installed extensions in `index.json` before being recorded at all: an unmatched name is dropped, the same way an unmatched built-in command already is, so a typo can never leak arbitrary text. A matched name is recorded as plain text, not hashed, specifically so extension popularity is visible — this knowingly relaxes gcx's existing telemetry privacy rule that no field may identify an organisation, since a real extension name can do exactly that. The bound: a new manifest field, `telemetry.reportUsage: false`, lets an author opt their extension's name out of being recorded at all.

Explicitly rejected or deferred, and why:

- **Flat top-level dispatch** (`gcx <name>`, gh/cargo/kubectl-style) — conflicts with CONSTITUTION.md's closed bare-verb enumeration; creates a permanent name-collision risk with gcx's own future areas; would need dynamic `rootCmd.AddCommand()` calls at every startup (more engineering than the chosen design, not less); and erases the at-a-glance signal that a command is third-party rather than built-in.
- **A separate `gcx ext run <name>` verb** — redundant once the fallback-dispatch mechanism already tells names apart from real verbs.
- **Direct credential handoff to extensions** (a `gcx config resolve-extension-credentials` command, manifest-declared permissions, tagging the caller) — designed, then cut, because "shell out to `gcx` itself" already solves the same problem with no new credential-resolution code path at all.
- **A "compile from source" fallback** when no manifest platform row matches — install just fails instead, keeping one predictable install code path.
- **`gcx ext scaffold`** (a Go-only starter-repo generator) — pure authoring convenience, not required for install/run to work. We can build this later if we need it.
- **A central, official discovery index, or `gcx ext search`** — discovery works however authors tell people about their extension. We can build this later if we need it.
- **A metadata-handshake protocol plus a synthesized per-extension Cobra tree**, so extensions show up in `gcx commands`/`gcx help-tree` with full parity to built-ins — real engineering weight (a new public `extsdk` package, runtime output-validation) that none of the four constraints actually call for.

## Consequences

**Easier:** third parties can extend gcx without needing gcx's approval, review, or hosting. A Go author's release process is "tag, push, `goreleaser release`, copy the checksums into the manifest" — gcx's own build matrix and release pipeline map directly onto what the manifest needs. Nothing about the mechanism is GitHub-specific, satisfying the hosting constraint concretely.

**Harder / accepted trade-offs:** there is no tooling to auto-generate manifests in v1, so an author must fill in the platform table and compute checksums themselves. There is no discovery mechanism beyond word-of-mouth.

The largest commitment is one this design makes implicitly: **gcx's JSON output shapes become the extension API surface.** Once "shell out to `gcx` and parse its JSON" is the sanctioned integration path, every envelope an extension depends on is someone else's dependency — and those envelopes are not uniform today (`datasources list` returns `{"datasources": [...]}`, `datasources delete` returns a bare array) and are covered by no schema or versioning promise. Accepted for v1, on the understanding that it fails silently: the first envelope change that breaks an extension will be found by an author, not by a test. `minGCXVersion` is the only tool an author has, and it is a blunt one. Revisit if the pattern gets real adoption.

**Extensions are invisible to agents, and gcx cannot promise the agent output contract on their behalf.** `gcx ext <name>` classifies as `raw` — the class already means "bytes gcx does not own", which is exactly true here — so it is exempt from the one-JSON-value rule the rest of the CLI follows. Installed extensions do not appear in `gcx commands` or `gcx help-tree` at all. An agent that finds `gcx setup datasources azure` in the command catalog today would find nothing for the same capability shipped as an extension. This is the direct cost of deferring the metadata handshake, and it is the reason a capability being implementable as an extension does not make it a good one.

**Nothing gcx installs is audited.** An extension runs with the user's full permissions on their machine, and the checksum only proves the bytes match what the manifest claimed, not that the manifest's author is trustworthy. This has to be prominent in user-facing docs and in `gcx ext install`'s own help, not left implicit in "no sandboxing".

**Nothing stops internal teams using this door for stable product capabilities.** The mechanism cannot tell a partner integration from a Grafana team shipping a feature outside the release process, and if that happens, commands under one `gcx` name will differ in authentication, context handling, output, errors, safety and support ownership. The mitigation is editorial rather than technical: this ADR says who the mechanism is for, and the boundary case in the Context section says what it is not for. An extension cannot get Grafana/Cloud data any other way than shelling out to `gcx` itself — an extension wanting a raw bearer token for its own HTTP client (a different language's SDK, a request gcx has no command for) isn't served, and would need this decision revisited if that need becomes real. Extensions cannot visually match gcx's own table/color styling. Recording a verified extension's name in telemetry is a deliberate, narrow exception to the existing "no field may identify an organisation" rule, not an oversight — an author who doesn't want their extension's name reported has to know to set `telemetry.reportUsage: false`; it isn't a default that protects them without action.

**Reference implementation:** [PR #1211](https://github.com/grafana/gcx/pull/1211) builds the mechanism and two extensions against it, and is where the local-build row, the argument boundary, the environment contract and the `update` semantics above came from. Its findings and the outline of an author guide live alongside it.

**Follow-up work:** a full PR-by-PR implementation sequence (manifest parsing → index management → local-path install → remote install → uninstall/update → CLI wiring → doc updates) is recorded in the research document's Implementation Plan section. Two pre-existing documentation gaps found during this work should be fixed independently of the feature: CONSTITUTION.md's existing use of "extension commands" for an unrelated concept (domain verbs like `irm incidents acknowledge`) needs a disambiguating note, and `docs/design/exit-codes.md` should document that `raw`-class commands (which already exist today — `gcx api`, `gcx alert *export`) are exempt from the 0-6 exit-code taxonomy.

Plausible future revisits, not committed to: a `gcx ext scaffold` Go starter-repo generator; a thin Go helper library for the "shell out and parse JSON" pattern (mirroring `gh.Exec` from `go-gh`, not its credential-reusing `api.DefaultRESTClient`); a narrower, purpose-built way for an extension to get a live Grafana/Cloud credential if a real author needs one gcx's own commands can't get them; auto-generating the manifest from GoReleaser output.
