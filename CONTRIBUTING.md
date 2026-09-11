# Contributing Guidelines

This document is a guide to help you through the process of contributing to `gcx`.

Before implementing features or commands, read:

- [ARCHITECTURE.md](ARCHITECTURE.md) — system architecture, pipeline diagrams, ADR index
- [DESIGN.md](DESIGN.md) — CLI UX design: command grammar, output model, taste rules
- [CONSTITUTION.md](CONSTITUTION.md) — invariants you must not violate
- [docs/design/](docs/design/) — prescriptive UX implementation rules (output, errors, agent mode, naming, …)

Naming a command? Start with the
[command naming and placement guide](docs/design/command-naming.md).

Adding, extending, or reviewing a gcx capability — a provider, datasource kind,
resource adapter, cloud command, or bundled skill? Ask your coding agent to use
the [`integrate-with-gcx`](.claude/skills/integrate-with-gcx/SKILL.md) skill. It
settles whether a new command is warranted at all and where it belongs, then
designs the command's agent-facing contract before any code gets written. It
hands implementation off to `add-provider` or `add-datasource` where those
apply, and runs a pre-review self-check over the finished diff.

## Code ownership

The code in this repository is owned by multiple teams. The ownership is codified in the [CODEOWNERS](./.github/CODEOWNERS) file. The @grafana/grafana-gcx team is responsible for the overall architecture of the repository, along with any features or functionality that are not specific to any particular provider.

### Product teams

Grafana engineering teams are welcome to contribute to and maintain their areas of the codebase without any interaction from the @grafana/grafana-gcx team. The [CODEOWNERS](./.github/CODEOWNERS) file should be such that these teams only need approvals from their own team to merge pull requests in their product area. If you find this is not the case, please do reach out to the @grafana/grafana-gcx team, or raise a pull request with a [CODEOWNERS](./.github/CODEOWNERS) change. If you are unsure, please reach out in the #gcx channel and we'd be happy to discuss.

We have tools in place to help maintain a consistent command surface and output conventions across the codebase, as well as LLM-assisted code review to try and ensure that the architecture and design conventions are followed. For more details on these tools, see:

- [The Claude review workflow](.github/workflows/claude-code-review.yml) reviews every non-draft PR opened by a person (bot PRs are skipped) with the repository's own [`review-pr`](.claude/skills/review-pr/SKILL.md) checks, so a PR gets the same treatment in CI as it does when a developer runs the review locally. The session returns its findings and the workflow publishes them as one comment review against the commit it reviewed, including a summary when there are no findings; missing or incomplete findings fail the run instead of publishing a clean review. Comment `@claude review` for a fresh review, and read the `claude-review-transcript-<PR>` artifact on the run to see what the session did. The review reads code: the normal CI jobs own builds, tests and generated docs.
- [Prefer existing command operations over creating new ones](docs/design/command-naming.md)  (test files are [here](cmd/gcx/root/commandoperations_test.go))
- [Syntax for experimental commands](docs/design/experimental-commands.md) (test files are referenced from the docs)


## Issue Tracking

Issues are tracked in [GitHub Issues](https://github.com/grafana/gcx/issues).
Use the issue templates when creating new issues - they set the correct issue
type and labels automatically.

## Making changes

### Agentic coding

If you are using a coding agent to make changes to this repository, there are skills in [.claude/skills](.claude/skills) for contributing:

- [add-provider](.claude/skills/add-provider) will help add a new top-level command area to gcx. 
- [add-datasource](.claude/skills/add-datasource) will help add a new datasource provider to gcx (under `gcx datasources`).
- [integrate-with-gcx](.claude/skills/integrate-with-gcx) is a more general skill that will help add capabilities with gcx.

### Development environment

`gcx` relies on [`mise`](https://mise.jdx.dev/) for tooling, and [Docker](https://docs.docker.com/get-started/get-docker/) for local development dependencies.

Install mise and set up the project. For macOS:

```console
$ brew install mise        # or: curl https://mise.run | sh
$ mise trust               # trust the mise.toml configuration
$ mise install             # install tools (Go, golangci-lint, etc.)
$ mise run deps            # install Go and Python dependencies
```

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

### Quick Start

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


### Stopping the test environment

When you're done testing, stop the services:

```console
$ mise run test-env-down
```

To remove all data (including database volumes):

```console
$ mise run test-env-clean
```

### Customizing the test environment

#### Modifying Grafana configuration

The Grafana instance uses a custom configuration file at `testdata/grafana.ini`. You can modify this file (you will need to restart the service)

```console
$ docker-compose restart grafana
```

#### Using a different Grafana version

To test against a different Grafana version, modify the `image` field in `docker-compose.yml`:

```yaml
services:
  grafana:
    image: grafana/grafana:12.1  # or any other version
```

Then restart the service:

```console
$ docker-compose up -d --force-recreate grafana
```

### View local grafana logs

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
