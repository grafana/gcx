# Contributing to gcx

You can contribute to gcx by participating in discussions, helping us triage existing issues, improving documentation, reporting bugs, suggesting new functionality or submitting code. We welcome your help, whether this is your first contribution or you've worked on gcx before.

For discussions, join `#gcx` on [Grafana Community Slack](https://grafana.slack.com/archives/C0AUPDVAGSC) or use [the GitHub discussions platform](https://github.com/grafana/gcx/discussions).

## Contents

- [Triaging issues](#triaging-issues)
- [Reporting bugs](#reporting-bugs)
- [Proposing new functionality](#proposing-new-functionality)
- [Contributing code](#contributing-code)
  - [Submitting a pull request](#submitting-a-pull-request)
  - [Using AI to contribute](#using-ai-to-contribute)
  - [Code ownership](#code-ownership)
- [Making changes](#making-changes)
  - [Development environment](#development-environment)
  - [Testing against a real Grafana API](#testing-against-a-real-grafana-api)
- [Releasing gcx](#releasing-gcx)

## Triaging issues

You can help us triage existing issues by voting for feature requests, reproducing bugs, identifying duplicates, or adding useful context in comments. This helps us prioritize and focus on the most important work.

You can also open new issues, but before you do so, please make sure to check the [existing issues](https://github.com/grafana/gcx/issues) and [pull requests](https://github.com/grafana/gcx/pulls). If you find a match, please add any useful context there, instead of opening new issues, to help us keep the discussion together.

## Reporting bugs

Please use the [Bug report form](https://github.com/grafana/gcx/issues/new?template=1-bug-report.yml). Describe what happened, what you expected, and the steps to reproduce it. Include your gcx version, the command you ran, and relevant error output. If you can't reproduce the problem reliably, share what you've observed. Please remove credentials and replace identifying details with placeholders before posting.

Please report security vulnerabilities privately through the [security policy](https://github.com/grafana/gcx/security/policy).

## Proposing new functionality

Have an idea for gcx? Use the [Feature proposal form](https://github.com/grafana/gcx/issues/new?template=2-feature-proposal.yml) to explain the problem you want to solve and show how the feature would work, with usage examples where helpful. The [project vision](VISION.md) and [design guidelines](DESIGN.md) can help you shape your proposal.

For a new command, consider its [name and placement](docs/design/command-naming.md) and whether it should be [experimental](docs/design/experimental-commands.md). If you have a technical approach in mind, include a short outline that fits the [architecture](ARCHITECTURE.md). You don't need a complete design to suggest an idea.

If you're unsure where your idea fits, ask us in Slack or [GitHub discussions](https://github.com/grafana/gcx/discussions). We're happy to help you work through it.

## Contributing code

You're welcome to open a PR directly for documentation changes and bug fixes, without an accepted issue. Please link an existing issue if there is one.

For other code contributions, please start with an issue accepted by the team. Leave a comment to let us know you would like to work on it so we can coordinate implementation and review with you.

For new features, please submit an RFC as a PR using the [RFC template](.agents/skills/create-rfc/references/rfc-template.md) before you start coding. This gives us a chance to agree on the design and how it fits with the rest of the project. See the [RFC guide](docs/rfcs/README.md) for details and [Code ownership](#code-ownership) for the product-team exception.

### Submitting a pull request

To help us review your PR, please:

1. Add or update tests and documentation for the behavior you changed.
2. Run `GCX_AGENT_MODE=false mise run all` to check skills, lint, tests, the build, and documentation. See [Development environment](#development-environment) for setup. Generated references are updated during releases; leave them unchanged in command PRs.
3. Sign your commits. See Grafana's [signed-commit requirement](https://community.grafana.com/t/action-required-signed-commits-mandatory-for-all-grafana-repositories/163404).
4. Use a descriptive PR title such as `fix(traces): handle empty results` or `docs: clarify setup` and the repository's [PR template](.github/pull_request_template.md).

Before we can merge your PR, the required CI checks need to pass and a code owner needs to approve it. Please work through actionable feedback from automated review and human reviewers, and ask if anything is unclear. If you are contributing from a fork, a maintainer may need to approve the CI runs.

### Using AI to contribute

You're welcome to use AI tools when contributing. Please make sure you understand, review, and test the changes you submit, just as you would with any contribution.

The repository's [`contribute`](.agents/skills/contribute/SKILL.md) skill can help your agent find the relevant guidance. Contributor skills live in `.agents/skills`; `.claude/skills` is a relative symlink to that directory, so both tools read the same files without a sync command. Edit skills in `.agents/skills` and preserve the symlink. As with `CLAUDE.md`, your Git checkout needs symlink support. You can use whichever development tools and planning approach work for you.

### Code ownership

[CODEOWNERS](.github/CODEOWNERS) identifies the teams responsible for each area. The @grafana/grafana-gcx team owns the overall architecture and shared features.

Grafana product teams can contribute within their existing owned area without prior agreement from the gcx team. Proposals are welcome there, but optional. The same review and quality requirements apply. Changes to shared commands, new top-level areas, or work outside the team's area need maintainer agreement.

If ownership is unclear or CODEOWNERS needs updating, please ask the gcx team in Slack or [GitHub discussions](https://github.com/grafana/gcx/discussions), or propose a CODEOWNERS change.

## Making changes

### Development environment

`gcx` relies on [`mise`](https://mise.jdx.dev/) for tooling and [Docker](https://docs.docker.com/get-started/get-docker/) for local development dependencies.

Install mise and set up the project. For macOS:

```console
$ brew install mise        # or: curl https://mise.run | sh
$ mise trust               # trust the mise.toml configuration
$ mise install             # install tools (Go, golangci-lint, etc.)
$ mise run deps            # install Go modules
```

Run `mise run deps` before `mise run all`. Documentation builds require Docker.
`mise run docs` builds `docs/sources` using Grafana’s website image without
regenerating reference files; `mise run serve-docs` starts a local preview.

Some mise commands for local development:

```console
$ mise run build           # build to bin/gcx
$ mise run lint            # run golangci-lint
$ mise run tests           # run all tests
$ mise run all             # lint + tests + build + docs
$ mise tasks               # list all available tasks
```

### Testing against a real Grafana API

While unit tests are valuable for testing individual components, integration testing against a real Grafana instance is important to ensure `gcx` works correctly with the actual Grafana API.

#### Start the test environment

The repository includes a `docker-compose.yml` file that sets up a complete test environment with:

- Grafana
- MySQL for storage
- Pre-configured with `admin:admin` credentials
- The `kubernetesDashboards` feature toggle enabled (required for `gcx`)

Run this with:

```console
$ mise run test-env-up
```

Check the status of the services with:

```console
$ mise run test-env-status
```

You can use the provided config file to get gcx to use the local Grafana instance. For example:

```console
$ go run ./cmd/gcx --config testdata/integration-test-config.yaml resources list-types
```

#### Stop the test environment

When you're done testing, stop the services:

```console
$ mise run test-env-down
```

To remove all data (including database volumes):

```console
$ mise run test-env-clean
```

#### Customize the test environment

##### Modify Grafana configuration

The Grafana instance uses a custom configuration file at `testdata/grafana.ini`. After editing this file, restart the service:

```console
$ docker-compose restart grafana
```

##### Use a different Grafana version

To test against a different Grafana version, modify the `image` field in `docker-compose.yml`:

```yaml
services:
  grafana:
    image: grafana/grafana:12.1 # or any other version
```

Then restart the service:

```console
$ docker-compose up -d --force-recreate grafana
```

#### View Grafana logs

To view logs from all services:

```console
$ mise run test-env-logs
```

To view logs from a specific service:

```console
$ docker-compose logs -f grafana
```

## Releasing gcx

### Generating a changelog and tagging

Releases are automated via `mise run tag`. It requires the `claude` CLI and [`svu`](https://github.com/caarlos0/svu).

```console
$ mise run tag -- patch   # or minor, major
```

This generates a changelog entry (via Claude), updates `CHANGELOG.md` and `.release-notes.md`, commits, tags, and pushes. The tag push triggers GoReleaser.

**With branch protection** (can't push directly to main): the script will fail at the push step. Instead:
1. Create a branch, commit the changelog, open a PR
2. Merge the PR
3. Tag the merge commit on main and push the tag:
   ```bash
   git checkout main && git pull
   git tag v0.X.Y
   git push origin v0.X.Y
   ```

### GoReleaser

Triggered automatically by the tag push via `.github/workflows/release.yaml`. GoReleaser builds binaries for all platforms and creates the GitHub release. No manual steps required. GoReleaser has no Homebrew role — the formula is rendered by a separate workflow (see below).

### Homebrew tap integration

After each stable release, `.github/workflows/publish-homebrew-formula.yml` runs automatically and opens a pull request against `grafana/homebrew-grafana` to add or update `gcx.rb` at the tap root (flat layout, alongside `alloy.rb`). The workflow:

1. Checks out gcx at the release tag.
2. Computes the SHA-256 of the GitHub-generated source tarball (`archive/refs/tags/vX.Y.Z.tar.gz`).
3. Renders `.github/homebrew/gcx.rb.tmpl` via `envsubst`, substituting `${VERSION}` and `${SHA256}`.
4. Clones the tap, commits the rendered `gcx.rb`, and opens a PR from a branch named `gcx-<version>`.

**The formula is not generated by GoReleaser.** If you're updating formula content, edit `.github/homebrew/gcx.rb.tmpl` directly — not `.goreleaser.yaml`. Pre-release tags (`v*-rc.*`, `v*-dev.*`) are skipped; the publish workflow exits cleanly for them.

If the workflow fails with a `401` or `permission denied` from `gh pr create` or `git push`, the App credentials used to authenticate against the tap have lapsed — check the Actions tab for the failing run and verify the secrets in the internal release runbook.

You can re-run the publisher for a past tag via the workflow's `workflow_dispatch` trigger (Actions tab → "Publish Homebrew Formula" → Run workflow → enter the tag).
