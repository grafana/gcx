# gcx as an embeddable Go library: typed API, not a CLI shim

**Created**: 2026-09-11
**Status**: proposed
**Supersedes**: none

## Context

The Grafana MCP server (mcp-grafana) wants to offer things gcx already knows how to do — SLO, Synthetic Monitoring, Fleet Management, and more — without writing its own REST client for each of those products from scratch. That's not a hypothetical want: SLO, Synthetic Monitoring, and k6 don't exist at all in mcp-grafana today. mcp-grafana already does this for a couple of other products by importing a ready-made Go library instead of hand-rolling a client — it uses `amixr-api-go-client` for OnCall and `incident-go` for IRM. We want gcx's other products to work the same way: something you can `import` and call, not something you have to talk to over a wire protocol you build yourself.

There's already an open, unreviewed pull request (gcx#558) that tried to solve this a different way. It added a function, `gcxlib.Execute(ctx, args, config)`, that runs gcx *as if from the command line*, in the same process: you hand it a list of strings like `["alert", "rules", "list"]`, and you get back whatever gcx would have printed to stdout, as JSON text. Looking closely at that approach turned up three real problems:

1. **Auth leaks through the cracks.** gcx has more than one internal place that decides "where do I get my Grafana credentials from." PR #558 patches two of them so they'll accept credentials handed to them programmatically instead of reading them from a config file on disk — and its own commit history shows it missed the second one on the first try. There's a third one, used by k6, Synthetic Monitoring's first-time setup, Faro's sourcemap upload, and Adaptive Telemetry, that still isn't patched at all. Anyone using that shim for those features would silently fall back to reading a config file that doesn't exist in an embedded program — and just fail, or worse, pick up whatever leftover config happens to be lying around on the host machine.
2. **It leans on global, mutable settings.** Every call to `Execute()` flips several settings that live outside any single request — things like "are we in agent mode," "is color output on." Right now every call happens to set them to the same values, so nothing's broken yet. But mcp-grafana genuinely runs multiple requests at the same time in the same process, so relying on "it happens to always agree with itself" is asking for a bug that only shows up under load.
3. **It doesn't actually save the work it promises to.** The whole pitch is "don't write a client for every product." But with this shim, mcp-grafana still has to know the exact command-line arguments for every operation and how to parse gcx's JSON output for every operation — it's a thinner client, not zero client.

Separately, mcp-grafana already handles multiple Grafana instances and users within one running server — each request carries its own URL, token, and org ID, pulled from HTTP headers. And mcp-grafana deliberately avoided using the standard Kubernetes Go library (`k8s.io/client-go`) for talking to Grafana's newer Kubernetes-style APIs, writing a much smaller hand-rolled version instead, specifically to keep its own dependency list light. gcx's dashboard/folder code does use `client-go` directly.

One more thing worth being upfront about: gcx's own foundational documents (the CONSTITUTION, which lists hard rules, and the VISION doc, which describes what gcx is for) don't currently say anything about gcx being usable as a library at all. They're not against it — they just don't mention it, because it wasn't a thing when they were written. VISION.md describes gcx as "a single CLI." That's the gap this decision is meant to close.

## Decision

gcx will offer a **typed Go API** — real functions with real input and output types — rather than the argument-list-in, JSON-text-out shim from PR #558. Each product gcx supports (starting with one, as a pilot) gets its own ordinary, importable Go package with real methods, the same shape as a normal Go SDK.

The specifics:

- **How auth works**: the caller hands over an already-configured `*http.Client` and a base URL. The library does not build its own network transport, does not go looking for a token anywhere, and does not read any config file. Whoever's calling the library is fully responsible for auth — exactly like using `amixr-api-go-client` or `incident-go` today.
- **Where the code lives**: in the same repo and the same Go module as the CLI, not a separate repository. And gcx's own command-line commands switch over to calling this new library **immediately**, product by product, as each one is built — never leaving two copies of the same logic (one for the CLI, one for the library) running side by side, even temporarily.
- **Version and stability**: one module, one version number, released together with the rest of gcx. There's no separate "library is still experimental" track — since the CLI itself will depend on it right away, it can't be shakier than the CLI.
- **What shape the packages take** (one package per product vs. one big combined client): deliberately not decided yet. It doesn't block anything else here, and the first product or two we actually build this for will make the right answer obvious.
- **What's included by default**: everything, except:
  - Things that only make sense from a terminal: `dev`, `login`, `cloud` (both the "manage Cloud stacks" and "cloud login" pieces), `config`, `setup`, `agent`, `skills`, `commands`, `helptree`, `version`.
  - Things tied to a Grafana Cloud account-level token rather than a single Grafana instance, which doesn't fit "caller hands us an `http.Client`": **k6**, **Faro's sourcemap upload**, **Adaptive Telemetry**, and specifically **Synthetic Monitoring's first-time token setup** (the rest of Synthetic Monitoring is fine and stays in). **Fleet Management is not excluded** — an earlier decision (ADR-023) already moved it off that account-level token onto the same per-instance auth everything else uses.
  - Two commands that can't work without a terminal no matter what: `resources edit` (opens your text editor) and `instrumentation setup` (an interactive yes/no wizard).
  - Two things that get *adjusted*, not dropped: a `--open`-a-browser flag on a few commands just returns the URL instead (it already does this in agent mode today). And commands that currently ask "are you sure? [y/N]" before deleting something (across IRM, alerting, SLO, the knowledge graph, dashboards, Synthetic Monitoring, and app observability) keep that ability in the library — they just take an explicit yes/no parameter instead of reading it from a keyboard, because leaving all of those out would make the library read-only for most products, which defeats the point.
- **Dashboards and folders** keep using `k8s.io/client-go` exactly as they do today. We're not rewriting that part to avoid the dependency.
- **Process going forward**: the checklist for adding a new product to gcx gets updated to require a typed library package and an immediate cutover of that product's CLI commands, as part of the same pull request — not "we'll get to it later."

### What we considered and said no to

- **PR #558's shim.** Rejected: it still leaves mcp-grafana writing a client per command, it depends on global settings that only work by coincidence today, and its auth-handoff mechanism has already needed one bug fix with a second, unfixed gap still open.
- **A brand new, separate repository just for this library.** Rejected for now, in favor of keeping it in the same repo — but not permanently ruled out, if the dependency situation turns out worse than expected once we're actually building it.
- **Rewriting dashboards/folders to drop the `k8s.io/client-go` dependency**, to match how mcp-grafana avoided it. Rejected — we're exposing what's there today as-is, and letting whoever uses the library decide if they're willing to take on that dependency for that one feature.
- **Figuring out the exact exclusion list later, as we go.** Rejected in favor of naming it up front (the list above), so it doesn't quietly grow every time someone forgets to check.
- **Leaving out every command that currently asks "are you sure?" before deleting something.** Rejected — those commands already behave safely without a terminal (they refuse instead of hanging, unless you pass a flag saying "yes, do it"). Cutting them all would make the library useless for anything beyond reading data.
- **Fixing every place gcx resolves credentials, as a single project, before writing any of this.** Considered, but not necessary right now: since we've already scoped out the features that use the still-unfixed path, and the library's own contract ("you hand us the http.Client") sidesteps the question entirely for everything that is in scope.

## Consequences

**Gets easier:**

- mcp-grafana (and anyone else) gets real access to SLO, Synthetic Monitoring, Fleet, and more, without writing a client for each one from scratch.
- There's never a moment where "what the CLI does" and "what the library does" for the same feature can drift apart — because there's only ever one implementation, and the CLI switches over the same day the library code is written.
- The library doesn't touch any of the settings that only make sense for a terminal (color, "am I in agent mode," etc.) at all — so the concurrency worry that PR #558 had just doesn't apply here; it's not a workaround, the problem doesn't exist in this design.
- It matches a pattern mcp-grafana is already comfortable with (import a typed SDK), instead of asking it to learn a new one.

**Gets harder:**

- This is genuinely new territory for gcx's own rulebook. The CONSTITUTION's rule that business logic belongs only in `internal/`, and the VISION doc's description of gcx as "a single CLI," were both written before this existed, and need updating to say how this new public surface fits in. (Listed as follow-up work below.)
- Code that gets promoted into this public library is, for the first time, making a real promise to people outside the gcx team. Because we're versioning it together with the CLI, a breaking change to the library is now automatically a breaking change to the next gcx release too — for everyone, not just library users.
- Dashboards/folders still require `k8s.io/client-go`. Anyone who wants that part of the library has to accept that dependency — mcp-grafana specifically chose not to for its own code, so this is a real cost for that one feature, not a clean win everywhere.
- Every future product added to gcx now has more required work up front — a library package and an immediate CLI cutover, not just the CLI part. If the checklist doesn't actually enforce this, the exclusion list will quietly grow the same way the unfixed credentials gap did.
- We haven't decided how the packages should be organized (one per product, or one shared client) — whatever we build first sets an unofficial precedent for everything after it, so that choice deserves real thought rather than just being copied forward.

**Follow-up work this creates:**

- Update VISION.md so it acknowledges gcx as an embeddable library, not only "a single CLI."
- Add a rule to CONSTITUTION.md explaining how this new public Go API relates to the existing "no business logic outside `internal/`" rule and to the CLI's own "stable within a major version" promise.
- Update the provider checklist and the `add-provider` skill so every new product requires a typed library package and CLI cutover, not a follow-up.
- Actually decide how the packages should be organized, either before or during the very first product we build this for.
