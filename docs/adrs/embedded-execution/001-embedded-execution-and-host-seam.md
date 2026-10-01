# Embedded execution and the `internal/host` seam

**Created**: 2026-10-01
**Status**: proposed
**Supersedes**: none

## Context

Agent hosts such as mcp-grafana want to expose gcx as a single escape-hatch
tool (`exec_gcx`): the model writes a gcx command line, the host runs it and
returns the output. The host is a long-lived, possibly multi-tenant server, so
each call must:

- use the caller's credentials, not whatever config, keychain or environment
  the server process happens to have;
- never read or write the server's filesystem, environment, stdio or argv,
  start processes, open listeners or install signal handlers;
- be safe to run concurrently with other calls in the same process;
- be limited to the HTTP access the caller was granted (read-only by default).

gcx was written as a CLI that owns its process: commands read `os.Getenv`,
write `os.Stdout`, open files and spawn browsers freely, and output state (agent
mode, TTY, color, klog) lives in package globals set per invocation.

[ADR-025](../declarative-provider-registration/001-declarative-resource-front-door.md)
and the typed-library work it enables give other programs a structured,
per-resource API. That is complementary: a typed API covers the resources
someone has wrapped, while an agent escape hatch needs the whole command
surface, including commands that will never get a typed equivalent.

## Decision

### Public in-process API: package `embed`

`embed.Run(ctx, commandLine, Options) (Result, error)` runs one gcx command
in-process and returns `Result{Stdout, Stderr, ExitCode}`.

- Each call builds a fresh cobra tree (`root.Command`), so flag state never
  carries between calls.
- Output is always agent-mode JSON, including the error document on failure.
- The command line is split with POSIX shell quoting rules but never passed to
  a shell. Pipes, redirects, command substitution and variable expansion are
  rejected with an error pointing at `--output`/`--jq`.
- An error return means the command could not start; a command that ran and
  failed returns a non-zero `ExitCode` and a nil error.

`embed` is the only public Go package. Everything else stays under
`internal/`.

### No command deny-list; a host seam instead

gcx does not maintain a list of commands that are unsafe to embed. A list
would drift as commands are added and could not see host access buried in
shared code paths (config loading, keychain migration, discovery caching,
update notifiers).

Instead, every host interaction goes through context-aware functions in
`internal/host`: filesystem, environment, argv, stdio, subprocesses
(`Command`, `LookPath`), listeners, signals, current user, working directory,
the OS keychain, fsnotify watchers and the client-go disk discovery cache. By
default each delegates to the `os`/`net`/`os/signal` equivalent. When the
context carries a `host.Sandbox`:

- filesystem, process, listener, signal, keychain and watcher operations
  refuse with `host.ErrUnavailable`;
- env, argv and stdio are served from the sandbox, never the process.

Filesystem refusals also satisfy `errors.Is(err, fs.ErrNotExist)`. Code that
treats a missing file as "use defaults" (config discovery, state files, caches)
therefore degrades gracefully, while code that genuinely needs a file fails
with an explanatory message.

The seam is enforced by lint, not convention. `.golangci.yaml`'s `forbidigo`
rules forbid the underlying `os`, `filepath`, `ioutil`, `os/user`, `os/exec`,
`net.Listen*`, `http.ListenAndServe`, `signal`, `syscall`/`unix`/`windows`,
`http.DefaultTransport`/`DefaultClient`, go-keyring, fsnotify and disk-cache
calls, and `os.Exit`. `depguard` confines libraries that touch the host on
their own (otel-checker) to the one command that wraps them in
`host.CaptureStdout`. Exemptions are limited to `internal/host`, tests,
`internal/testutils`, `scripts/`, and `os.Exit` in `cmd/gcx/main.go`.

### Credentials from memory

`config.ContextWithInMemoryConfig` makes `LoadLayered` resolve a supplied
config instead of discovering files. An explicit `--config` and `GCX_CONFIG`
(read from the sandbox environment) still take precedence, and overrides such
as `--context` still apply.
`embed.Run` builds a single-context config from `Options` (Grafana URL,
token or basic auth, org/stack, TLS, optional Grafana Cloud token).

A new `grafana.headers` config field adds headers to every Grafana request,
for authenticating proxies and on-behalf-of credentials.

### HTTP access levels

`host.Access` is the most a sandboxed invocation may do over HTTP:
`AccessRead` (the zero value: GET/HEAD/OPTIONS), `AccessWrite` (adds POST,
PUT, PATCH) or `AccessDelete` (full). `host.GuardTransport` enforces it as
the outermost transport on every gcx HTTP client; a denied request is never
sent. The rule mirrors the Grafana Assistant gcx proxy's method-based scope
check: the method decides, except for an allowlist of read-shaped POSTs
(datasource queries, searches, validate-only calls, and the read methods of
RPC-style APIs that send every call as POST) that count as reads. Outside a
sandbox the guard is a pass-through.

### Process-wide output state

Agent mode, TTY/truncation, color, style, the klog logger and telemetry are
process globals. Package `embed`'s `init` fixes the output state once (agent
mode on, piped, no truncation, no color or style). When the context is
sandboxed the root `PersistentPreRun` skips per-invocation output
configuration, leaves the global klog logger alone (client-go logs through
the contextual logger instead) and records no telemetry, so concurrent calls
do not race on any of them.

### Backstop test

`embed/commands_test.go` walks the command tree and runs every leaf command
embedded, read-only, against a test server that answers every request. It
asserts that no command sends a write, hangs, writes to the process's stdio,
or writes to disk. This is the safety net that replaces a deny-list: a new
command that bypasses the seam fails CI.

### Rejected alternatives

- **Subprocess exec of the gcx binary.** Requires shipping a binary next to
  the host, and a child process inherits the host's filesystem, environment
  and network, so isolation would rely on OS sandboxing outside gcx's control.
  Per-call process start-up is also expensive.
- **Deny-list of unsafe commands.** Drifts with every new command and misses
  host access in shared code paths; see above.
- **Injecting only a `NamespacedRESTConfig`.** Covers K8s-tier commands but
  not providers that read the full config (Cloud tokens, provider settings,
  stack discovery). The in-memory `Config` reaches every loader.
- **Typed library only (ADR-025 direction).** Complementary, not a
  replacement. The typed library is the structured tier; `embed` is the
  escape-hatch tier for everything without a typed equivalent.

## Consequences

### Positive

- One Go call gives an embedder the full command surface with per-call
  credentials, captured output and an HTTP access ceiling.
- Host isolation is structural: new code cannot reach the host without going
  through `internal/host`, and the backstop test catches what lint cannot.
- The CLI's behaviour is unchanged outside a sandbox.

### Negative

- Every host-touching call now needs a `context.Context`, which threaded
  contexts through code that previously had none.
- `embed` is a public API with compatibility expectations.
- Commands that need files, browsers, listeners or the keychain do not work
  embedded. File input must come through stdin (`-f -`).

### Known limits and follow-ups

- Outbound requests to user-supplied URLs (for example Synthetic Monitoring
  target validation) are not host-restricted. Hosted embedders should treat
  this as an SSRF surface and restrict egress at the network layer.
- The backstop test uses placeholder arguments, so only a subset of commands
  reach the network; the rest fail argument validation first. Write-safety for
  those relies on `GuardTransport` alone.
- The read-shaped POST allowlist must be kept in step with the Assistant gcx
  proxy.

## Proposed constitution amendment

Requires human approval; not yet applied to [CONSTITUTION.md](../../../CONSTITUTION.md).
Add under the architecture invariants:

> **All host access goes through `internal/host`.** Code outside
> `internal/host` (other than tests, test helpers, `scripts/`, and `os.Exit`
> in `cmd/gcx/main.go`) must not touch the filesystem, environment, argv,
> stdio, subprocesses, network listeners, signals, OS keychain, file watchers
> or `http.DefaultTransport`/`http.DefaultClient` directly. Every such call
> takes a `context.Context` and must behave correctly — refuse, or serve from
> the sandbox — when that context carries a `host.Sandbox`. Enforced by
> `forbidigo`/`depguard` in `.golangci.yaml`.
