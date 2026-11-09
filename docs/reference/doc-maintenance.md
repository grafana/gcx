# Documentation maintenance

Update documentation when a change affects what readers need to know. Explain behavior, architecture boundaries, contracts, and tradeoffs; use the source tree for package, file, command, and dependency inventories.

## What to update

- **User-visible behavior:** update the relevant guide in `docs/sources/` or `docs/reference/`, including examples that change. Update `README.md` only when the introduction or quick start changes.
- **Architecture or shared contracts:** update the relevant `docs/architecture/` or `docs/design/` document. Update `ARCHITECTURE.md` when system boundaries or extension points change. A new package alone does not require a documentation update.
- **Contributor workflows:** update `CONTRIBUTING.md`, `AGENTS.md`, or the affected contributor skill where that guidance lives. `CLAUDE.md` is a symlink to `AGENTS.md`.
- **Design proposals and decisions:** follow the [RFC conventions](../rfcs/README.md). Keep status, rationale, terminology, and meaningful implementation evidence in the RFC. Update current architecture documentation when a design is implemented, not merely proposed.

## Keep maintenance proportional

Give each rule one home and link to it instead of copying it. Keep root documents concise entry points. Link directly to source directories, commands, or configuration when readers need a current inventory; do not maintain package maps, file catalogues, command trees, or RFC lists by hand.

Keep navigation that helps readers find a topic, without requiring every file to appear in several indexes. Avoid volatile counts, dependency versions, timestamps, and progress inventories that repeat repository state. Historical decision dates and concrete compatibility constraints remain useful.

When moving or deleting a document, repair incoming links and references in contributor skills, code comments, and repository configuration as well as other docs. Preserve decision rationale and distinguish proposed, accepted, implemented, superseded, and retired work. Keep restricted research outside this public checkout.

## Verification

Check changed examples and links against the source and run `mise run docs` to validate the Grafana website documentation. The website build covers `docs/sources/`; check contributor-document links separately.

Do not regenerate `docs/sources/cli-reference.md` in command PRs. `mise run tag` refreshes the released reference in the release changelog PR; use `mise run docs:refresh` for standalone updates to the latest stable release. Test generator changes with `go test ./scripts/...`.
