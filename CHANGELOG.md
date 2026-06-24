## Unreleased

## v0.4.1 (2026-06-23)

- **Breaking:** `--log-http-payload` has been renamed to `--insecure-log-http-payload`. The old flag name now exits with an error naming the replacement. Payload-dump behavior is unchanged — credentials, cookies, OAuth refresh tokens, and request bodies still appear in the dump when the flag is set. A startup warning is now printed to stderr when the flag is active.
- **Breaking:** `gcx dev serve --address` now defaults to `127.0.0.1` (loopback) instead of `0.0.0.0`. Users who need LAN or remote access should pass `--address 0.0.0.0` explicitly. The WebSocket livereload endpoint now rejects cross-origin requests from hosts other than `localhost`, `127.0.0.1`, `[::1]`, or the configured listen address.
- **Soft-breaking:** Custom Rego rules loaded via `gcx dev lint --rules` can no longer call `http.send`, any `net.*` builtin, or `opa.runtime`. No flag is provided to re-enable them. Bundled rules are unaffected.
- Added `--jq` for inline JSON transformation and improved agent-mode hints.
- Added OAuth login support for agent and non-interactive workflows.
- Improved config performance with lazy keychain checks and StackID caching.
- Moved stack management under the `gcx cloud stacks` command group.
- Added App O11y service list, get, map, and operation breakdown commands.
- Added Instrumentation Hub `gcx instrumentation check` diagnostics.
- Expanded Assistant investigations with Lodestone v2 capability detection.
- Expanded Knowledge Graph diagnose, meta metrics, and model-rules support.
- Improved IRM incident filtering, paging, and OnCall escalation output.
- Added metrics instant-query timestamps and richer Tempo tag-value output.
- Hardened local debug surfaces and JSON error output for agent workflows.

## v0.4.0 (2026-06-02)

- Added `gcx assistant conversation list` and `gcx assistant conversation get` to inspect Assistant conversations and transcripts.
- Added support for continuing accessible non-CLI Assistant conversations by context ID, with a warning when the source is not CLI.
- Added automatic migration of token-shaped config secrets into the OS keychain, with plaintext fallback when the keychain is unavailable.
- **Breaking**: renamed `gcx kg rules` to `gcx kg prom-rules` to align with `model-rules` and `relabel-rules`.
- Added read-only `gcx kg relabel-rules get` for prologue, epilogue, and generated rule groups.
- Fixed `kg diagnose` verdicts so failing checks no longer report a healthy service, and reclassify scoped no-data checks as label-scope warnings when unscoped data exists.
- Shared Grafana datasource query transport and dataframe wire types across ClickHouse, InfluxDB, and Infinity clients.
- Hardened Claude GitHub Actions secret handling by scoping Vault outputs and restricting `@claude` triggers to trusted repository associations.
- Improved agent guidance for dashboard workflows and Tempo trace analysis, including recommending `gcx traces get --llm -o json` for full-trace analysis.
- Removed several small direct dependencies in favor of internal implementations.

## v0.3.0 (2026-05-26)

- Added ClickHouse, CloudWatch, and Infinity datasource providers.
- Added richer IRM OnCall alert groups and SRE triage tooling.
- Added a create-dashboard skill and dashboard list --limit flag.
- Improved KG diagnosis for trace propagation and split environments.
- Fixed KG rule list/get/delete calls to match the backend API.
- Added dual k6 client support for OAuth and service-account tokens.
- Fixed k6 provider support for OAuth login.
- Made aio11y experiment cancellation require confirmation.
- Warn when API responses return HTML instead of JSON.
- Exit cleanly on Ctrl+C without noisy errors.
- Normalized dashboard snapshot time ranges.
- Removed target-valid-logql rule to restore go install compatibility.
- Updated login help and Homebrew publishing.

## v0.2.16 (2026-05-20)

- Add `aio11y experiments` command group for managing evaluation experiments
- Add `aio11y guards` subcommand for managing hook rules
- Fix `kg insights` chart and sources request/response schemas
- Fix `k6` token piping warning to reference the correct command
- Centralize signal command wiring across metrics, logs, traces, and profiles
- Consolidate error types into `internal/gcxerrors`, removing `fail` shims
- Surface `diagnose-entity-graph` and document how skills get invoked
- Mint Homebrew tap App token via broker in release workflow
- Replace Dependabot with Renovate for dependency updates
- Update Go module dependencies


## v0.2.15 (2026-05-18)

- **New**: `gcx instrumentation` command tree — clusters, services, setup, status
- **New**: InfluxDB datasource provider
- **New**: `gcx irm incidents contexts list` command
- **New**: Knowledge Graph `diagnose` command
- Profiles query: add `--profile-id` and `--stacktrace-selector` flags
- Profiles query: add pprof output format
- Profiles query: hint at `profile-types` command in `--profile-type` help
- **Breaking**: rename `kg health` to `kg summary` with restructured output
- **Breaking**: remove duplicate `kg scopes` command (use `kg entities scopes`)
- **Breaking**: remove UI-centric `kg insights` query/summary/graph commands
- **Breaking**: move `cypher` under `kg entities cypher`
- **Breaking**: unify insight filtering under `kg entities list --insight`
- Surface propagated assertions in `kg entities list`
- Add insight filter flags to `kg entities inspect`
- Improve `kg entities` help text and surface scope props in schema
- Fix `kg insights search` endpoint to `/v1/assertions/search`
- Fix `config use-context` to write to the right file when a local `.gcx.yaml` is loaded
- Fix `login` to derive context name from `--server` when no name is given
- Fix datasource kind normalization to recognize Prometheus flavor plugins
- Eliminate redundant datasource GET after auto-discovery
- Include valid values in enum-shaped error messages
- Remove superseded `gcx setup instrumentation` subtree
- Refactor pyroscope query to use Options pattern
- Docs: document `--time` flag for instant queries on `explore-datasources`
- Docs: add manifest examples to `gcx irm incidents create`
- Docs: move mounting docs to public documentation; fix broken anchor
- Add CODEOWNERS with product team co-ownership
- Add docs sync to the website repo on merge to main


## v0.2.14 (2026-05-08)

- **New**: Instrumentation Hub provider package with full CRUD, RMW, and
  Helm formatter support
- **New**: Alert provisioning CRUD — contact-points, mute-timings,
  notification-policies, and templates
- **New**: AI Observability saved-conversations and collections commands
- **New**: `gcx version` structured subcommand with machine-readable output
- **New**: `gcx assistant dashboard` subcommand; fix `--agent-id` flag
- **New**: Login accepts `--org-id` to configure organization ID
- Knowledge Graph entities list now supports pagination
- Knowledge Graph inspect drops hardcoded filters for raw, agent-friendly output
- Agents codec with temp-file spill for token-efficient agent output
- Log failed agent invocations to disk for capability-gap analysis
- Fix exit codes: usage errors emit 2, partial failures emit 4
- `stacks delete`: rename `--yes` to `--force`; respect agent mode
- Migrate all provider delete commands to consistent `ConfirmDestructive`
- Fix non-interactive confirmation bypass for metrics adaptive and alert
- Config check now classifies `.grafana.com` hosts and stack-id as Cloud
- Login now suggests running `config check` after successful login
- Fix IRM incident URL template to use correct OnCall plugin slug
- Dev import: register v1 converters for Folder and Dashboard resources
- `--json list` field discovery now returns all nested paths recursively (previously limited to top-level + one level of `spec.*`). Users relying on `gcx resources get --json list` or `gcx resources schemas --json list` will see a larger field set.


## v0.2.13 (2026-05-06)

**Note**: This release includes two important bugfixes 

- Fix `--dry-run` not being honored in resource delete operations. [PR #643](https://github.com/grafana/gcx/pull/643).
- Fix `--context` flag not applied across all CRUD adapter operations. [PR #625](https://github.com/grafana/gcx/pull/625).

Update to this version to avoid unintended operations on your Grafana Cloud stack.

---

Other changes in this release:

- Add `gcx stacks` commands: list, get, create, update, delete, regions
- Rename `synth` provider to `synthetic-monitoring`
- Render trace trees as a formatted table in `gcx traces get`
- Add RCA Workbench deep link to `gcx kg entities inspect`
- Consolidate Knowledge Graph insights filtering into `kg entities list`
- Prevent env var secrets from being written to the config file
- Handle read-only files gracefully during skill updates
- Update agent skills to remove common usage errors
- Bump Go module and GitHub Actions dependencies



## v0.2.12 (2026-05-04)

- **Dashboards**: new CRUD, search, and version history provider
- **Dashboards**: dev server syncs variable params to URL and restores on refresh
- **Knowledge Graph**: add `suppressions list` and `suppressions delete` commands
- **Knowledge Graph**: fix suppressions overwrite bug
- **Knowledge Graph**: replace `kg inspect` with `entities inspect` (LLM summary)
- **Knowledge Graph**: align all kg commands to `[noun] [verb]` format
- **Knowledge Graph**: improve `entities list` usability
- **Login**: support custom OAuth callback port via `--callback-port`
- **Login**: bind OAuth callback port before opening browser to avoid race
- **Login**: step-aware errors during connectivity validation
- **Profiles**: add exemplars support (`exemplars profile` and `exemplars span`)
- **Datasources**: add Grafana Explore share links for query results
- **Notifications**: alert users when a new gcx version is available
- **Skills**: notify users when installed skills have updates
- **Assistant**: gracefully block commands on self-hosted Grafana instances
- **Linter**: detect missing title/description on panels in collapsed rows
- **Tooling**: replace devbox with mise for dev environment setup


## v0.2.11 (2026-04-29)

- Add mTLS client certificate authentication for config and login (Teleport)
- Add `kg describe` command for schema, scopes, and telemetry configs
- Add `skills update` command to update existing installed skills
- Fix metrics default view to be usable out of the box
- Fix synthetic monitoring to surface required scopes on register/install failure
- Fix grafana-com instance selector regression
- Fix `config set/unset` to resolve bare paths against the current context
- Fix front matter in the debug-with-grafana skill
- Update README with sigil/aio11y rename and restored Compatibility section
- Update Go dependencies, GitHub Actions, and MySQL Docker tag to v9.7
- Remove PyPI publishing job from release CI

## v0.2.10 (2026-04-23)

- Replace Homebrew binary cask with source formula for Gatekeeper-free macOS installs
- Add automated workflow to publish Homebrew formula on release
- Update installation docs and README with new Homebrew instructions


## v0.2.9 (2026-04-23)

- Consolidated `gcx auth` and `gcx config` into a unified `gcx login` command
- Renamed `gcx sigil` command and provider to `gcx aio11y` (AI Observability)
- Fixed `gcx irm` to pass `--max-age` filter through to the OnCall backend
- Added PyPI publishing to the release workflow
- Bumped Claude plugin version automatically on release
- Added Grafana Cloud API tiers architectural overview to the docs
- Added compatibility matrix to the README


## v0.2.8 (2026-04-20)

- Rename `gcx sigil` command and provider to `gcx aio11y` (AI Observability)
- Fix OAuth refresh lockout when running multiple gcx invocations concurrently
- Improve typed API error handling for datasource queries
- Rename OnCall/Incidents references to IRM across docs and CLI
- Default SLO definitions list limit to all results
- Add Homebrew installation support with docs
- Allow login through grafana.com/launch
- Unified CLI UX consistency pass across commands
- Reorganise and clean up README
- Add DatasourceProvider interface and plugin system for datasources
- Add billing subtree and generic series leaf to metrics
- Add --from/--to time range flags to all kg commands
- Validate kg --scope flag values against known scopes
- Remove redundant kg search entities command
- Filter incidents by tags and from/to time range
- Add fleet auth error scopes suggestion
- Add sigil skill to claude-plugin
- Guide agents to use Grafana Assistant for reasoning tasks
- Recognise OPENCODE as an agent mode
- Bump Kubernetes dependencies to v0.35.4 and Docker deps
- Update anthropics/claude-code-action workflow digest


## v0.2.7 (2026-04-15)



- Default `gcx slo definitions list --limit` to 0 (print all SLOs); raise agent `token_cost` to medium with hint to use `--limit` when narrowing output
- Consolidate OnCall + Incidents under unified `irm` provider
- Add adaptive metrics segments and exemptions commands
- Adopt server-side pagination for list commands
- Auto-discover Synthetic Monitoring URL from plugin settings
- Improve skills list output, add installed status, single-skill install
- Fix adaptive telemetry auth when using OAuth for Grafana
- Suggest `stacks:read` scope on cloud stack lookup 403
- Update OAuth coverage warning to remove incidents/oncall
- Align assistant SSE HTTP client timeout with `--timeout` flag
- Fix `gcx dev serve` not exiting on Ctrl+C
- Fix watcher error channel handling
- Trim Knowledge Graph CLI surface and typed resources
- Add marketing bento-box slide with verified CLI commands
- Upgrade ASCII logo to ANSI Shadow font
- Use "k6" instead of "K6" in UI text
- Restructure README for better narrative flow
- Dependency updates (Go modules, GitHub Actions)


## v0.2.6 (2026-04-13)



- Add `--limit` flag with default pagination (50) to all list commands
- Add retry transport for rate limiting and transient HTTP errors
- Unified HTTP client construction with debug logging
- Set consistent User-Agent header on all HTTP clients
- Add `alert instances list` with server-side state filtering
- Route OnCall requests through OAuth proxy
- Add `skills install` command for .agents-compatible harnesses
- Add `--expr` flag alias for datasource query commands
- Add curl-pipe installer script with shell-specific PATH instructions
- Fix config context selection before env overrides in provider loaders
- Fix SLO definitions commands not inheriting parent config loader
- Restore shell tab-completion
- Add Fish shell completion docs
- Update Go and Docker dependencies


## v0.2.5 (2026-04-10)



- Rename `faro` CLI command to `frontend`
- Auto-derive context name from server URL during login
- Add OAuth experimental warning to login flow
- Add `assistant:chat` scope to OAuth flow
- Add HTTP traffic debug logging
- Add Sigil generations, scores, and judge commands
- Add latency and reachability to synth checks status
- Add access property to datasource list and get
- Centralized agent annotations with consistency tests
- Fix null stream labels and missing content in log queries
- Improve human-readable logs query output
- Require `--instant` flag for TraceQL instant metrics
- Fall back to `/api/ds/query` for Loki and Prometheus
- Resolve datasources across all API groups
- Make config edit resilient to broken configs
- Fix invalid CLI commands in docs and skills


## v0.2.4 (2026-04-08)



- Add sigil evaluator/rule CRUD and templates commands
- Add sigil agents and eval read-only commands
- Add synthetic monitoring private probe management
- Restructure adaptive metrics command layout
- Promote `--json ?` as primary discovery for programmatic use
- Reject stray arguments on group commands
- Improve error messages for wrong/unknown commands
- Fix graph output for non-series query results
- Fix empty timestamp display in traces instant tables
- Fix synth check status to use alertSensitivity thresholds
- Include alerting enrichments in SLO definitions get/list
- Add titles to all issues
- Restructure docs into VISION, ARCHITECTURE, DESIGN split
- Fix command syntax and install instructions in README

## v0.2.3 (2026-04-07)



- Fix OAuth token persistence on refresh
- Add styled tables and ASCII logo with Neon Dark theme
- Add assistant investigation CRUD commands
- Improve agent discoverability with progressive disclosure
- Fix error propagation in natural key matching
- Add natural key matching for cross-stack resource push
- Add adaptive log drop-rules CLI and client
- Add datasource autodiscovery
- Update Kubernetes and CI dependencies
- Improve auth login and README documentation


## v0.2.2 (2026-04-03)

- Add Grafana Assistant prompt command (A2A protocol)
- Add Faro (Frontend Observability) provider
- Add Sigil AI observability provider with conversations
- Add Tempo trace query commands (search, get, metrics, tags)
- Lift signal commands to top-level (metrics, logs, traces, profiles)
- Add gcx-observability skill for Claude plugin
- Improve auth login error when server is missing
- Trim trailing slash from server URL in config
- Centralize --json field selection in provider commands
- Remove kg service-dashboard command
- Align datasource query docs with Loki terminology
- Recommend manual token config over auth login in docs


## v0.2.1 (2026-04-02)

- Add automated release process with AI-generated changelogs
- Remove Knowledge Graph (kg) env commands


## v0.2.0 (2026-04-02)

- Add OAuth browser-based login for Grafana (`gcx auth login`)
- Add declarative instrumentation setup (`gcx setup`)
- Add Pyroscope SelectSeries support with time-series and top modes
- Add adaptive logs exemptions & segments CLI
- Add adaptive traces policy CRUD commands
- Rename KG assertions commands to insights
- Fix synthetic monitoring check management UX
- Fix version info for `go install` builds
- Fix stack status DTO handling
- Fix Loki query usage errors
- Remove KG frontend-rules command

## v0.1.0 (2026-03-30)

- Initial release of gcx (formerly grafanactl)
- K8s resource tier: get, push, pull, delete, edit, validate, serve via Grafana K8s API
- Cloud provider tier with pluggable providers: SLO, Synthetic Monitoring, OnCall, Fleet, Knowledge Graph, Incidents, Alerting, App O11y, Adaptive Telemetry
- Datasource queries: Prometheus, Loki, Pyroscope
- Dashboard snapshots via Image Renderer
- Resource linting engine with Rego rules and PromQL/LogQL validators
- Agent mode with command catalog and resource type metadata
- Config system with named contexts, env var overrides, TLS support
- Live dev server with reverse proxy and websocket reload
- Output codecs: JSON, YAML, text, wide, CSV, graph
- CI/CD with conventional commits, golangci-lint, reference doc drift checks
