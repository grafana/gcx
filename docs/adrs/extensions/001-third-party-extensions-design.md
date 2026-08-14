# Third-party extensions: manifest-driven install, no credential handoff

**Created**: 2026-08-14
**Status**: proposed
**Supersedes**: none

## Context

gcx has no way for third parties to add their own subcommands today. The CLI tree is built entirely at compile time (`cmd/gcx/root/command.go`), and providers self-register through a single `init()` call each.

The goal is a third-party extension mechanism with four high level goals: easy to author, must run on every OS/arch gcx itself ships for (`linux/darwin/windows` × `amd64/arm64`), must not force extensions to be hosted in one particular place, and must not require a second binary to find or manage them (thinking about [Krew](https://krew.sigs.k8s.io/)).

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
- **Manifest-driven, not name-convention-driven.** Each extension ships a `gcx-extension.yaml` (matching gcx's own `apiVersion`/`kind`/`metadata` shape) declaring version, a `minGCXVersion` constraint, and a `platforms` table listing a URL + sha256 + binary name per OS/arch — resolved declaratively, the same way krew's plugin manifest works, with no download logic the author has to write and no "compile from source" fallback if no row matches. A `script` field lets a non-compiled extension (bash, Python, anything) skip the platform table entirely.
- **Any hosting location.** `gcx ext install <source>` accepts a local path, a direct URL to the manifest, or any git URL — resolved by shelling out to the user's own `git`, never a GitHub API call. No central index is required for an extension to be installable.
- **No separate `run` verb.** `ext` registers `install`/`list`/`uninstall`/`update` as real subcommands; anything else in the first argument position is treated as an extension name and looked up in `index.json`. This is the same "known verbs win, anything else is a name" pattern git uses at its own top level.
- **No credential handoff.** Extensions never receive a Grafana or Cloud token directly, in any form. Running an extension sets `GCX_EXT_GCX_BIN` on its environment (the path to the invoking `gcx` binary, the same idea as Docker's `DOCKER_CLI_PLUGIN_ORIGINAL_CLI_COMMAND`); an extension that needs Grafana/Cloud data shells out to that binary and consumes its JSON output (`gcx api`, `gcx resources get`, any provider command with `--output json`), inheriting whatever auth and refresh logic gcx already handles. This mirrors how gh extensions commonly call `gh api` rather than handling GitHub auth themselves.
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

**Harder / accepted trade-offs:** there is no tooling to auto-generate manifests in v1, so an author must fill in the platform table and compute checksums themselves. There is no discovery mechanism beyond word-of-mouth. An extension cannot get Grafana/Cloud data any other way than shelling out to `gcx` itself — an extension wanting a raw bearer token for its own HTTP client (a different language's SDK, a request gcx has no command for) isn't served, and would need this decision revisited if that need becomes real. Extensions cannot visually match gcx's own table/color styling. Recording a verified extension's name in telemetry is a deliberate, narrow exception to the existing "no field may identify an organisation" rule, not an oversight — an author who doesn't want their extension's name reported has to know to set `telemetry.reportUsage: false`; it isn't a default that protects them without action.

**Follow-up work:** a full PR-by-PR implementation sequence (manifest parsing → index management → local-path install → remote install → uninstall/update → CLI wiring → doc updates) is recorded in the research document's Implementation Plan section. Two pre-existing documentation gaps found during this work should be fixed independently of the feature: CONSTITUTION.md's existing use of "extension commands" for an unrelated concept (domain verbs like `irm incidents acknowledge`) needs a disambiguating note, and `docs/design/exit-codes.md` should document that `raw`-class commands (which already exist today — `gcx api`, `gcx alert *export`) are exempt from the 0-6 exit-code taxonomy.

Plausible future revisits, not committed to: a `gcx ext scaffold` Go starter-repo generator; a thin Go helper library for the "shell out and parse JSON" pattern (mirroring `gh.Exec` from `go-gh`, not its credential-reusing `api.DefaultRESTClient`); a narrower, purpose-built way for an extension to get a live Grafana/Cloud credential if a real author needs one gcx's own commands can't get them; auto-generating the manifest from GoReleaser output.
