# Research: gcx Extensions Feature

> The accepted decision is recorded in [ADR-023](../adrs/extensions/001-third-party-extensions-design.md). This document is the background research and implementation plan behind it, not a second source of truth for the decision itself.

**Created**: 2026-08-14
**Confidence**: High - five tools checked against their own docs/source, plus direct reading of this repo's code and rules.
**Sources**: 6 (gh CLI, kubectl/krew, cargo, Docker CLI plugins, Helm, gcx codebase)

## Executive Summary

- gcx needs a way for third parties to extend it. Four rules: easy to write, must run on every OS/arch gcx ships for, must not force one hosting site, must not need a second binary to manage it.
- krew breaks rule 4 (it's its own binary) and, in practice, rule 3 (its main index needs a human-reviewed PR to be listed, though custom indexes work). Helm's install-hook model is the worst for security - a real CVE let it skip signature checks when a file was just missing.
- gcx's own build rules some options out. The CLI tree is built at compile time. Providers register themselves in code. The build has no CGO and targets 6 platforms. So Go's native plugin system (`.so` files) can't work here. Extensions have to be separate programs.
- The plan: a manifest-driven design, close to krew's. Install with `gcx ext install <source>` (a local path, a direct URL, or any git host - no GitHub required). Run with `gcx ext <name>`. Extensions never get Grafana or Cloud credentials directly - if they need that access, they shell out to the `gcx` binary itself, which is already logged in.
- `gcx ext scaffold` (a Go starter-repo generator) is cut from v1. It's a nice-to-have, not something the mechanism needs to work.
- A bigger, more complex design (a handshake step, extensions showing up in `gcx commands`/`gcx help-tree` just like built-in commands, a public SDK package) was worked out too, but it's not part of v1. It solves a problem nobody asked to solve.

## Findings

### gh CLI extensions

Repos named `gh-<name>`. gh finds installed ones from its own record, not by scanning PATH. A binary extension needs release files named `<name>-<os>-<arch>`; a script extension needs no build step at all (`gh extension create` starts you with a bash script). Anyone can host one, on any git server. But gh only *lists* extensions tagged with a GitHub topic - others still install fine, they just don't show up in search. No sandboxing. The child process gets the parent's full environment. Extensions get GitHub access by calling `gh api`/`gh auth token` themselves, not through special env vars.

### kubectl + krew

kubectl just scans PATH for `kubectl-<name>` programs. Flat names, and plugins can't override built-in commands. krew is a genuinely separate binary - the exact thing gcx's rule 4 rules out. It manages its own folder and adds a symlink folder to PATH; it's just a layer on top of the same PATH mechanism kubectl already has. krew's plugin file is fully declarative: for each OS/arch it lists a URL, a checksum, and the binary name - krew picks the right one and does the download itself, no code the author has to write. The best idea in the whole survey. Its main index needs a human-reviewed PR to be listed, but custom indexes work too (`kubectl krew index add <name> <git-url>`) - GitHub is a convention here, not a requirement.

### cargo

`cargo-<name>` on PATH (cargo checks its own bin folder first). `cargo install` builds from source by default - from crates.io, a git URL, or a local path, all fully supported - so it works on any platform for free. Prebuilt binaries are a separate community add-on (`cargo-binstall`). No way to browse extensions at all - `cargo --list` doesn't even show descriptions for them (a long-standing complaint). Very little is passed to subcommands (just the path to `cargo` itself); anything that needs project info has to call `cargo metadata`.

### Docker CLI plugins

`docker-<name>` programs found in a few fixed folders - not a PATH scan, on purpose. Docker has a handshake: it calls `docker-<name> docker-cli-plugin-metadata`, and the plugin prints a small JSON description back. That's how `docker --help` lists plugins without any built-in registry - the second-best idea in the survey. There's no install command at all; you (or a package manager) just put the binary in the right folder. No update checking either. Docker ships an official Go helper for the handshake, so Go is the easy choice, though not required.

### Helm

Plugins live in their own folder, no PATH involved. Each plugin's `plugin.yaml` has an install step that's just an arbitrary shell command the author writes to fetch a binary - much more work for the author than krew's approach, and a real security risk (arbitrary shell code runs on install; a disclosed bug let signature checks pass even when the signature file was simply missing). No central, reviewed list - discovery is just an informal community page.

### gcx codebase

- The CLI tree is built entirely at compile time. `cmd/gcx/root/command.go` wires up every command by hand; providers register themselves through a single `init()` call each - a hard rule in CONSTITUTION.md. Nothing loads commands at runtime today.
- `commands.Command` and `helptree.Command` (the agent-facing command list) walk the finished command tree once, at build time. They don't rescan later.
- The build has `CGO_ENABLED=0` and targets 6 OS/arch combos through GoReleaser. That rules out Go's native plugin system (`.so` files), which needs CGO and an exact version match. Extensions have to be separate programs, not code loaded into the same process.
- The closest thing gcx already has, `cmd/gcx/skills`, only copies static files bundled into the binary onto disk - it never runs third-party code. Good precedent for "install and place files," no precedent for "find and run someone else's program."
- CONSTITUTION.md's grammar rule is strict: commands are `$AREA $NOUN $VERB` (or `$AREA $VERB`), and bare top-level commands are a closed list (`login`, `setup`, `version`, `help`, `completion`). That rules out gh/cargo/kubectl's flat `<tool>-<name>` style outright.
- CONSTITUTION.md already uses the words "extension commands" for something else - domain verbs like `irm incidents acknowledge`. This feature's own docs should always say "third-party extensions" to avoid confusion.
- DESIGN.md requires every command to declare an output shape (checked in CI) and use a fixed set of exit codes.
- The release pipeline already uses GoReleaser, which builds for every platform and writes checksums - exactly what an extension manifest needs.
- `cmd/gcx/dev/scaffold.go` (and `generate.go`, `import.go`) all use the same trick: bundle template files into the binary, then write them out with Go's `text/template`. Reusable if we ever build a scaffold command.

## Recommendations

### Naming and invocation

A new top-level area, `ext` (package `cmd/gcx/ext`, sitting next to `dev`/`config`/`agent`):

```
gcx ext install <source>       # fetch, verify, extract, register
gcx ext list                   # installed extensions: name, version, source, description
gcx ext uninstall <name>
gcx ext update [<name>] [--all]
gcx ext <name> [args...]       # run an installed extension
```

(`gcx ext scaffold` is cut for v1 - see "Explicitly out of scope for v1.")

There's no separate `run` verb. `ext` registers `install`/`list`/`uninstall`/`update` as real subcommands. If the first argument doesn't match one of those, `ext` treats it as an extension name and looks it up in `index.json`. Everything after the name goes straight to the extension untouched - gcx doesn't parse its flags. This is the same trick git uses: known verbs win, anything else is a name.

One consequence: an extension can't be named `install`, `list`, `uninstall`, or `update` - that's checked at install time. `ext --help` should list installed extensions directly (read from `index.json`, no extra cost) so people can find this without guessing.

Running an extension also sets `GCX_EXT_GCX_BIN` on its environment - the path to the `gcx` binary that launched it (same idea as Docker's `DOCKER_CLI_PLUGIN_ORIGINAL_CLI_COMMAND`). That's how an extension reaches Grafana or Cloud APIs - see "Reaching Grafana and Cloud APIs" - without gcx handing it a credential directly.

This whole thing is one new fallback path on the `ext` command, marked once in `cmd/gcx/root/testdata/output_classes.json` as `"gcx ext": "raw"` - the same class as `gcx api`. No extra entries needed per extension, since gcx never adds per-extension commands to its own tree, and `commands.Command`/`helptree.Command` don't need to change.

Two other ways to invoke extensions were considered and turned down - see "Explicitly out of scope for v1."

### Discovery at runtime

No PATH scanning - that would pollute the user's PATH and risk clashing with unrelated programs. Instead, a dedicated gcx folder, matching how config already works (`internal/config/loader.go`):

- `~/.config/gcx/extensions/<name>/<version>/` - the extracted files.
- `~/.config/gcx/extensions/index.json` - the only thing `gcx ext list`/`gcx ext <name>` reads: a list of `{name, version, description, source, bin_path, installed_at}`. One file read, no folder scan, no extra process just to list. Only `install`/`uninstall`/`update` write to it.
- A new `GCX_EXTENSIONS_DIR` env var can override the folder, matching `GCX_CONFIG`.

### Manifest and distribution

Each extension ships a `gcx-extension.yaml` file at its root, matching gcx's own Kubernetes-style shape:

```yaml
apiVersion: gcx.grafana.com/v1
kind: Extension
metadata:
  name: acme-widgets
  description: Manage Acme widget resources from gcx
version: 1.4.0
minGCXVersion: 1.8.0
homepage: https://git.example.com/acme/gcx-ext-acme-widgets
platforms:
  - selector: {os: linux, arch: amd64}
    uri: https://.../acme-widgets_1.4.0_linux_amd64.tar.gz
    sha256: 9f2b...
    bin: acme-widgets
  # ...darwin/amd64, darwin/arm64, linux/arm64, windows/amd64
```

A script extension that needs no build step can skip `platforms` and just set `script: hello.sh` instead - gcx runs that file directly (the author is responsible for a correct shebang line).

`gcx ext install <source>` figures out what kind of source it got, the same way `cargo install`/`go install` do:

- **A local path** (folder or archive) - no network, useful for testing.
- **A direct URL to the manifest** - any static host works, no API needed. This is the real answer to "don't force a hosting location": nothing here is GitHub-specific.
- **A git URL** (any host) - clones or fetches with the user's own `git`, then reads the manifest from the repo. No GitHub API call anywhere.

What happens on install: fetch the manifest, check it's valid and that gcx is new enough, find the row matching the current OS/arch, show an install plan (name, version, homepage, download URL, checksum), ask for confirmation (same rule as deleting cloud resources - skip with `--force`/`--yes`/`GCX_AUTO_APPROVE`, and agent mode always needs `--force`), download, check the checksum (fail hard on any mismatch or if it's missing), extract, and write the `index.json` entry. Nothing runs at install time - actually using the extension (`gcx ext <name>`) always happens later, as its own step.

Cross-platform support works like krew: the author lists a URL and checksum per platform, and gcx just picks the right one - no download logic the author has to write, and no "compile it yourself" fallback (to minimise scope). gcx's own release pipeline already builds for the same 6 platforms and writes checksums, so an author using Go can copy gcx's own `.goreleaser.yaml` and get this almost for free. If no platform row matches, install just fails - there's no fallback to building from source, so there's only one install code path to maintain.

Manifests are written by hand for v1 - the author fills in `platforms` and its checksums themselves (easy to copy from GoReleaser's own checksum file). Auto-generating this is a later step - see "Explicitly out of scope for v1."

An author can also add an optional `telemetry` block to opt out of usage reporting - see "Usage telemetry" below.

### Authoring experience

The smallest possible extension is one program (or script) plus one `gcx-extension.yaml` file. There's no required file name (unlike `gh-<name>` etc) since gcx finds extensions through the manifest.

**No scaffolding tool in v1, for any language.** The manifest format already works for any language for free - a `platforms` entry just points at a binary, and the `script` field covers anything that doesn't need a build step. So "any language can write an extension" costs nothing extra; gcx just runs whatever `bin` or `script` says. An author writes their own manifest and program by hand and copies gcx's own `.goreleaser.yaml` as a starting point for building it. No template generator ships with v1.

A future `gcx ext scaffold` is a reasonable next step later - see "Explicitly out of scope for v1."

### Security posture

- No sandboxing in v1 - the extension gets the same full environment as its parent process, same as every tool we looked at.
- **No credential handoff at all.** Extensions never get a Grafana or Cloud token directly, in any form - see "Reaching Grafana and Cloud APIs."
- The install confirmation shows name, version, homepage, download URL, and checksum, and needs explicit approval - the same disclosure every surveyed tool uses. There's no permissions list to show, because there's no credential handoff to limit.
- Checksum checks are required and fail hard for anything not local.
- No signature checks beyond the checksum in v1.

### Reaching Grafana and Cloud APIs

gcx already has commands that talk to Grafana and Cloud APIs as the logged-in user - `gcx api` (raw passthrough), `gcx resources get`/`list`, and every provider command with `--output json`. An extension that needs that data just runs the `gcx` binary itself and reads its JSON output, the same way gh extensions usually call `gh api`/`gh issue list --json` instead of handling GitHub auth on their own.

How: when `gcx ext <name>` runs an extension, it sets `GCX_EXT_GCX_BIN` on its environment - the path to the `gcx` binary doing the running (same idea as Docker's `DOCKER_CLI_PLUGIN_ORIGINAL_CLI_COMMAND`). An extension that wants Grafana data runs `$GCX_EXT_GCX_BIN resources get ... --output json` (or any other command) as its own subprocess, and gets whatever auth and refresh logic gcx already handles. No new credential code, no permissions model, no token freshness to worry about - because the extension never touches a token.

One known gap, accepted for v1: this doesn't help an extension that wants a raw token for its own HTTP client - a different language's SDK, say, or a request shaped like nothing gcx already has a command for. That's a real need, but not a confirmed one yet. Better to solve it properly later if a real extension author hits this wall than build credential-handling machinery against a guess now.

### Usage telemetry

gcx already sends anonymous usage telemetry for every command (`internal/telemetry`). Each invocation emits one event carrying things like the resolved command path, whether it succeeded, how long it took, and a random per-install ID that isn't tied to any account. The user can turn this off entirely (`telemetry: disabled` in config, or `GCX_TELEMETRY`). Extension dispatch hooks into this exact same pipeline - same event shape, same opt-out - rather than building a separate one.

What gets recorded is narrow and specific: **`gcx ext <name>` - the command and the extension's name, and nothing else.** Any arguments after the name are never recorded, for the same reason gcx never records flag values today - they're free-form and could contain anything.

Before recording the name, gcx checks it against the installed extensions in `index.json`. If it doesn't match anything actually installed - a typo, or garbage - it's dropped, the same way an unmatched built-in command is today (gcx already avoids recording raw mistyped text; this reuses that exact handling). Only a name that matches something the user genuinely installed gets recorded.

That name is recorded as plain text, not hashed - on purpose, so gcx can actually see which extensions are popular, which needs a readable name rather than an opaque value nobody can look up. **This is a deliberate, narrow exception to gcx's existing telemetry rule that no field may identify a person or an organisation** - a real extension name can reveal what tooling an org has built, and that's accepted here as a trade-off, not an oversight. The thing that keeps this bounded: an author can opt their extension's name out of being recorded at all, with a new manifest field:

```yaml
telemetry:
  reportUsage: false   # default true
```

This is the main safeguard for anyone who doesn't want their extension's name showing up in gcx's stats - since the default is now to report it, this flag is the one lever an author has, not just a nice-to-have.

### Explicitly out of scope for v1

- An extension index, or `gcx ext search` - discovery works however authors tell people about their extension, the same trade-off cargo makes. We could build a list somewhere in the future.
- Making extensions show up in `gcx commands` or `gcx help-tree` just like built-in commands(a new public `extsdk` package, checking each extension's output at runtime). Worth doing later if people need it.
- Checking an extension's output against DESIGN.md's output rules. Those rules are enforced in CI for gcx's own commands, and can't be forced onto arbitrary third-party programs. `gcx ext` is a documented as an exception to that rule.
- Sandboxing, keeping more than one version installed at once, checking for updates in the background, and shell completion for extension names.
- Any signature check beyond the checksum.
- A "build from source" fallback when no platform row matches - install just fails instead. One code path, no extra logic to detect toolchains.
- Auto-generating the manifest from GoReleaser's output (`gcx ext manifest generate --from-goreleaser`) - v1 manifests are written by hand; revisit once real authors feel that friction.
- **`gcx ext scaffold`** (a Go-only starter-repo generator, reusing the template trick from `cmd/gcx/dev/scaffold.go`) - a nice-to-have, not required for install/run to work. Worth building once a real Go author is hand-writing manifests and feels the pain.
- **Handing extensions Grafana/Cloud credentials directly** (a `gcx config resolve-extension-credentials` command, manifest-declared permissions, tagging the caller): I explored this,then cut it. Extensions that need Grafana/Cloud access shell out to `gcx` itself instead (see "Reaching Grafana and Cloud APIs"), so none of this is needed. Worth revisiting only if someone needs a raw token gcx's own commands can't get them. Omitting this reduced the scope significantly.

### Compliance check

- **CLI grammar** - fits cleanly. `ext` is a new area (`$AREA $VERB`, same as `dev`/`config`), not a new bare verb. No rule change needed.
- **Naming clash** - CONSTITUTION.md already uses "extension commands" for something else (domain verbs like `irm incidents acknowledge`). Not a real conflict, but worth a docs note so nobody confuses the two; this feature should always say "third-party extensions."
- **Output/exit-code rules** - one new entry in `output_classes.json`, marked `raw`. DESIGN.md doesn't currently say that `raw`-class commands skip the normal exit-code rules, even though some already do (like `gcx api`) - worth fixing that gap while we're here.
- **Provider rules** (`providers.Register()`, `TypedCRUD`, the adapter registry) - don't apply. Extensions must never reach `internal/providers` or the adapter registry. Worth stating outright so it's never quietly extended later.
- **Layer rules** - `cmd/gcx/ext/*.go` stays thin wiring; all real logic (manifest parsing, checksums, the index, running the extension) lives in `internal/ext/*.go`.
- **Dependencies** - no new one needed. Reuses `github.com/Masterminds/semver/v3` (already in `go.mod`) for version checks, shells out to the user's own `git` for git installs, and uses the standard library for tar/zip/gzip/sha256.

### Critical files for implementation

- `cmd/gcx/root/command.go` - where the new `ext` area gets mounted.
- `cmd/gcx/root/testdata/output_classes.json` - add the one `"gcx ext": "raw"` entry.
- `internal/config/loader.go` - the folder-resolution pattern to copy for `internal/ext.ExtensionsDir()`.
- `internal/skills/install.go` - the closest existing pattern for install/index logic, to adapt into `internal/ext/install.go` and `internal/ext/index.go`.
- `.goreleaser.yaml` - the 6-platform build/checksum matrix that defines the target platform set and gives authors something to copy.
- `go.mod` - confirms `github.com/Masterminds/semver/v3` is already there.
- `CONSTITUTION.md` (CLI grammar section) - needs the naming-clash note above.
- `docs/design/exit-codes.md` - needs the `raw`-class exemption gap documented.
- `cmd/gcx/root/telemetry.go` and `internal/telemetry/event.go` - the existing telemetry pipeline `gcx ext <name>` hooks into; recording the extension name needs the `index.json` lookup wired in here.

For the later `gcx ext scaffold` (not needed for v1): `cmd/gcx/dev/scaffold.go` (+ `cmd/gcx/dev/templates/`) - the template pattern to reuse when it's built.

### Verification (once implementation begins)

- `go build -buildvcs=false -o bin/gcx ./cmd/gcx/` builds cleanly with `ext` wired in.
- End to end by hand: write a small script extension and `gcx-extension.yaml` (no scaffold in v1) → `gcx ext install <local-path>` → `gcx ext list` shows it → `gcx ext acme-widgets -- --hello` runs it and passes through args/exit code → `gcx ext uninstall acme-widgets` removes it from `index.json` and deletes its files.
- Test a git-URL install and a direct-manifest-URL install against a scratch repo, to confirm nothing GitHub-specific gets hit.
- Test that a checksum mismatch, and a missing checksum, both fail hard.
- Test that `GCX_EXT_GCX_BIN` is set correctly when an extension runs, and that a script extension calling `$GCX_EXT_GCX_BIN resources get ... --output json` (or similar) works using the user's own logged-in session.
- Test that running an installed extension records a telemetry event with the extension's name; test that running an unmatched/typo'd name (or one whose manifest sets `telemetry.reportUsage: false`) records nothing.
- `go test ./cmd/gcx/ext/... ./internal/ext/...`, then a full `mise run gate`, before calling this done.

## Implementation Plan

A sequence of small PRs, each one buildable and testable on its own. Nothing gets mounted into the live CLI (so no doc-regen or CI surface changes) until PR 6, which keeps PRs 1-5 small and low-risk. Each PR after PR 0 only depends on the one right before it.

**PR 0 — Fix two existing doc gaps.** No dependency on anything else, can land anytime. Add the CONSTITUTION.md note telling "extension commands" (the existing phrase, domain verbs like `irm incidents acknowledge`) apart from this feature's "third-party extensions." Also document in `docs/design/exit-codes.md` that `raw`-class commands (confirmed to already exist today - `gcx api`, `gcx alert *export`) are exempt from the 0-6 exit-code rules. Pure docs, no code, no risk. Touches CONSTITUTION.md, so flag it for explicit sign-off, separate from the feature itself.

**PR 1 — Manifest parsing** (`internal/ext/manifest.go`). Parse `gcx-extension.yaml`, check required fields, check `minGCXVersion` with the semver library already in `go.mod`, and pick the right `platforms` row for the current OS/arch (or fall back to `script`). Plain functions, no I/O beyond the bytes it's given, fully unit-tested. Nothing calls it yet.

**PR 2 — Extensions folder and index** (`internal/ext/index.go`). `ExtensionsDir()`, copying the folder-resolution pattern from `internal/config/loader.go` (and honoring a new `GCX_EXTENSIONS_DIR`), plus reading and writing `index.json`. Depends on PR 1 only for the shape of an index entry.

**PR 3 — Install from a local path** (`internal/ext/install.go`). The full pipeline - read the manifest, pick the platform or script, copy/extract the files, check the checksum, show the confirmation prompt (reusing the existing safety-prompt helper), write the index entry - but for local paths only. Keeps this PR small and lets the whole pipeline be tested with no network involved.

**PR 4 — Install from a URL or git host.** Adds to PR 3: fetching over HTTP (size and time capped) and cloning with the user's own `git` (`git clone --depth=1` / `git archive`). This is the PR that actually proves "no forced hosting location." Split from PR 3 because it's a different kind of risk - network and subprocess calls, not just local logic.

**PR 5 — Uninstall and update.** Small, since install and the index already exist by now.

**PR 6 — Wire it into the CLI** (`cmd/gcx/ext`). The `install`/`list`/`uninstall`/`update` subcommands, the plain `gcx ext <name>` dispatch (`DisableFlagParsing`, runs the extension as a subprocess, sets `GCX_EXT_GCX_BIN`), the telemetry hook (verify `<name>` against `index.json`, record `gcx ext <name>` unless the manifest sets `telemetry.reportUsage: false`), mounting `ext` in `cmd/gcx/root/command.go`, the `"gcx ext": "raw"` entry in `output_classes.json`, and a CLI reference doc regen. This is the first PR where the feature actually works end to end - but since all the logic already landed in PRs 1-5, this one stays thin wiring, matching the rule that `cmd/` code is wiring only.

**PR 7 — Update the architecture docs.** Add `cmd/gcx/ext`/`internal/ext` to the package map in `docs/architecture/project-structure.md`, per CLAUDE.md's rule that architecture docs must stay current.

## Sources

1. [Using GitHub CLI extensions](https://docs.github.com/en/github-cli/github-cli/using-github-cli-extensions) — install/discovery model
2. [Creating GitHub CLI extensions](https://docs.github.com/en/github-cli/github-cli/creating-github-cli-extensions) — authoring/scaffold
3. [gh extension | GitHub CLI manual](https://cli.github.com/manual/gh_extension)
4. [gh extension install | GitHub CLI manual](https://cli.github.com/manual/gh_extension_install)
5. [New GitHub CLI extension tools — GitHub Blog](https://github.blog/developer-skills/github/new-github-cli-extension-tools/)
6. [Extension Management — cli/cli DeepWiki](https://deepwiki.com/cli/cli/4.6-extension-management)
7. [gh-extension-precompile README](https://github.com/cli/gh-extension-precompile/blob/trunk/README.md) — multi-arch release asset convention
8. [Fun With GitHub CLI Extensions — meiji163](https://meiji163.github.io/post/gh-extension/)
9. [GHSA-p2h2-3vg9-4p87](https://github.com/cli/cli/security/advisories/GHSA-p2h2-3vg9-4p87) — trust-model context
10. [Krew installation docs](https://krew.sigs.k8s.io/docs/user-guide/setup/install/)
11. [kubectl plugins — Kubernetes docs](https://kubernetes.io/docs/tasks/extend-kubectl/kubectl-plugins/)
12. [Krew plugin manifest developer guide](https://krew.sigs.k8s.io/docs/developer-guide/plugin-manifest/) — declarative platform selector
13. [Krew custom indexes](https://krew.sigs.k8s.io/docs/developer-guide/custom-indexes/) — non-GitHub hosting support
14. [krew-index README](https://github.com/kubernetes-sigs/krew-index/blob/master/README.md)
15. [Submitting plugins to Krew](https://krew.sigs.k8s.io/docs/developer-guide/release/new-plugin/)
16. [GoReleaser Krew customization](https://goreleaser.com/customization/publish/krew/)
17. [krew-release-bot](https://github.com/rajatjindal/krew-release-bot)
18. [Trend Micro: Protecting Your Krew](https://www.trendmicro.com/vinfo/us/security/news/cybercrime-and-digital-threats/protecting-your-krew-a-security-analysis-of-kubectl-plug-ins) — supply-chain risk
19. [kubernetes-sigs/krew GitHub repo](https://github.com/kubernetes-sigs/krew)
20. [External Tools — The Cargo Book](https://doc.rust-lang.org/cargo/reference/external-tools.html)
21. [cargo install — The Cargo Book](https://doc.rust-lang.org/cargo/commands/cargo-install.html)
22. [Environment Variables — The Cargo Book](https://doc.rust-lang.org/cargo/reference/environment-variables.html)
23. [Third party cargo subcommands (wiki)](https://github.com/rust-lang/cargo/wiki/Third-party-cargo-subcommands)
24. [cargo#10662 — `cargo --list` third-party descriptions](https://github.com/rust-lang/cargo/issues/10662)
25. [cargo#8842 — env passed to subcommands](https://github.com/rust-lang/cargo/issues/8842)
26. [cargo-bins/cargo-binstall](https://github.com/cargo-bins/cargo-binstall)
27. [cargo-bins/cargo-quickinstall](https://github.com/cargo-bins/cargo-quickinstall)
28. [nabijaczleweli/cargo-update](https://github.com/nabijaczleweli/cargo-update)
29. [Rust/Cargo supply chain security](https://www.systemshardening.com/articles/cicd/rust-cargo-supply-chain-security/)
30. [docker/cli manager.go](https://github.com/docker/cli/blob/master/cli-plugins/manager/manager.go) — discovery dirs
31. [docker/cli manager_unix.go](https://github.com/docker/cli/blob/master/cli-plugins/manager/manager_unix.go)
32. [docker/cli plugin.go](https://github.com/docker/cli/blob/master/cli-plugins/plugin/plugin.go) — metadata handshake framework
33. [docker/cli metadata package](https://pkg.go.dev/github.com/docker/cli/cli-plugins/metadata)
34. [docker/cli manager package](https://pkg.go.dev/github.com/docker/cli/cli-plugins/manager)
35. [CLI Plugins Design — docker/cli#1534](https://github.com/docker/cli/issues/1534)
36. [Docker CLI Plugins — DeepWiki](https://deepwiki.com/docker/docker-ce-packaging/6.3-docker-cli-plugins)
37. [Plugin Architecture — DeepWiki](https://deepwiki.com/docker/cli/3-plugin-architecture)
38. [Homebrew Formulae: docker-buildx](https://formulae.brew.sh/formula/docker-buildx)
39. [Enabling Docker CLI plugins from Homebrew](https://darren.oh.name/node/82)
40. [Docker Compose as a Docker CLI plugin (gist)](https://gist.github.com/thaJeztah/b7950186212a49e91a806689e66b317d)
41. [The Helm Plugins Guide](https://helm.sh/docs/topics/plugins/)
42. [helm plugin install](https://helm.sh/docs/helm/helm_plugin_install/)
43. [Plugins User Guide](https://helm.sh/docs/plugins/user/)
44. [Community/Related Helm plugins list](https://helm.sh/docs/community/related/)
45. [HIP-0026: Wasm plugin system](https://helm.sh/community/hips/hip-0026/) — Helm's own admission of Helm 3 gaps
46. [helm-www plugins.mdx source](https://github.com/helm/helm-www/blob/main/docs/topics/plugins.mdx)
47. [helm/helm#30876](https://github.com/helm/helm/issues/30876) — platformCommand/command migration break
48. [helm/helm#30920](https://github.com/helm/helm/issues/30920)
49. [helm/helm#13640](https://github.com/helm/helm/issues/13640)
50. [helm/helm#30985](https://github.com/helm/helm/issues/30985)
51. [GHSA-q5jf-9vfq-h4h7](https://github.com/helm/helm/security/advisories/GHSA-q5jf-9vfq-h4h7) — verification fails open
52. [Artifact Hub: Helm plugins](https://artifacthub.io/docs/topics/repositories/helm-plugins/)
53. [helm.sh/helm/v3/pkg/plugin](https://pkg.go.dev/helm.sh/helm/v3/pkg/plugin)
54. gcx codebase — `CONSTITUTION.md`, `DESIGN.md`, `cmd/gcx/root/command.go`, `internal/providers/registry.go`, `internal/providers/provider.go`, `cmd/gcx/skills/command.go`, `internal/skills/install.go`, `cmd/gcx/dev/scaffold.go`, `.goreleaser.yaml`, `mise.toml`, `.claude/skills/release/SKILL.md` (direct source reading, 2026-08-14)
