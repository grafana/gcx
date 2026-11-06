# Documentation maintenance

Update documentation when a change affects what readers need to know. Explain behavior, architecture boundaries, contracts, and tradeoffs; use the source tree for package, file, command, and dependency inventories.

## Where documentation lives

- `docs/sources/` is the public documentation for people using gcx, including user-facing reference material such as configuration keys, linter rules, and costs. `mise run docs` publishes this tree to the Grafana website.
- `docs/contributing/` is the documentation for people changing gcx: workflows, conventions, and implementation guides. It is not part of the website build.
- `docs/architecture/` and `docs/design/` hold the mechanics behind the rules in `ARCHITECTURE.md` and `DESIGN.md`.
- `docs/reference/` is being retired. Do not add documents there; its files move into `docs/sources/` or `docs/contributing/` by audience.

## What to update

- **User-visible behavior:** update the relevant guide in `docs/sources/`, including examples that change. `README.md` is the product tour: update it when the behavior it shows changes, not merely because behavior changed somewhere.
- **Architecture or shared contracts:** update the relevant `docs/architecture/` or `docs/design/` document. Update `ARCHITECTURE.md` when system boundaries or extension points change. A new package alone does not require a documentation update. Architecture documents describe patterns and anything that guides an implementation decision, not the structure of individual providers; the code is a provider's reference. Framework wiring is documented only where getting it wrong fails silently.
- **Contributor workflows:** update `CONTRIBUTING.md`, `AGENTS.md`, or the affected contributor skill where that guidance lives. `CLAUDE.md` is a symlink to `AGENTS.md`.
- **Design proposals and decisions:** follow the [RFC conventions](../rfcs/README.md). Keep status, rationale, terminology, and meaningful implementation evidence in the RFC. Update current architecture documentation when a design is implemented, not merely proposed.

## Writing rules

`ARCHITECTURE.md` and `DESIGN.md` state each rule and its consequence: what you must do and what breaks otherwise. The document below them holds the mechanics of following it. Neither restates the other's half.

State the rule the project wants, not a description of today's code. Where existing code violates a rule, new code still follows it, and the violation is migration debt: track it in an issue or a test allowlist, never by naming violators in the rule's text.

## Keep maintenance proportional

Give each rule one home and link to it instead of copying it. A pointer is a link plus at most one sentence saying why to follow it; it never restates the rule. Keep root documents concise entry points. Link directly to source directories, commands, or configuration when readers need a current inventory; do not maintain package maps, file catalogues, command trees, or RFC lists by hand.

Keep navigation that helps readers find a topic, without requiring every file to appear in several indexes. Avoid volatile counts, dependency versions, timestamps, and progress inventories that repeat repository state. Historical decision dates and concrete compatibility constraints remain useful.

When moving or deleting a document, repair incoming links and references in contributor skills, code comments, and repository configuration as well as other docs. Preserve decision rationale and distinguish proposed, accepted, implemented, superseded, and retired work. Keep restricted research outside this public checkout.

## Verification

Check changed examples and links against the source and run `mise run docs` to validate the Grafana website documentation. The website build covers `docs/sources/`; check contributor-document links separately.

Do not regenerate `docs/sources/cli-reference.md` in command PRs. `mise run tag` refreshes the released reference in the release changelog PR; use `mise run docs:refresh` for standalone updates to the latest stable release. Test generator changes with `go test ./scripts/...`.
