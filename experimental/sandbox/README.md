# experimental/sandbox

> **Experimental.** This module is `v0`, and its API may change without notice.

Package `sandbox` runs gcx commands in an in-process sandbox, so any Go program
can embed gcx as a function call:

```
args, env, stdin  ──►  gcx (WebAssembly, in-process)  ──►  stdout, stderr, exit code
                              │
                              └─ every HTTP request is handed to the host
```

It's a separate Go module (`github.com/grafana/gcx/experimental/sandbox`), so it
adds nothing to the gcx CLI's build or dependencies.

## Why

We want to embed gcx in other programs, for example as a tool in Grafana's hosted
MCP server. The obvious approaches both fall short:

- **Importing gcx as a library.** gcx is a CLI with process-global state: cobra
  command trees, package-level transports and config, `os.Exit`, writes to stdout
  and `$HOME`. Making it safe to call repeatedly, concurrently and for different
  users in one long-lived process means a large refactor, plus ongoing discipline
  to keep it that way.
- **Running gcx as a subprocess.** In a multitenant service each invocation runs
  as the server's OS user, so it can read the server's files, environment and
  credentials, and reach any network address.

Compiling gcx to WebAssembly (`GOOS=wasip1`) and running it with
[wazero](https://wazero.io), a pure-Go runtime, keeps the subprocess model (the
unmodified CLI, a fresh instance per command) with in-process isolation:

- **Nothing is shared between calls.** Each command gets a new instance with its
  own memory, args, env and filesystem view.
- **Nothing is reachable unless granted.** The guest sees an empty read-only `/`,
  one writable `$HOME`, only the environment variables it's given, and no sockets.
- **The host controls the network.** gcx hands every HTTP request to the host,
  which applies an egress policy and can add credentials the guest never sees.
- **Pure Go.** No cgo and no external runtime.

## How it works

- **gcx side** (`internal/httputils/wire_wasip1.go`). `httputils.WireTransport` is
  the innermost layer of every gcx HTTP client. In normal builds it does nothing.
  Under `GOOS=wasip1` it serializes each request as HTTP/1.1 and passes it to the
  host's `gcx_http` import module:
  - `start` begins a request and returns an ID
  - `poll` reports whether it has finished
  - `take` copies the response into a buffer the guest provides
  - `cancel` abandons the request

  Requests from gcx's goroutines run concurrently on the host. Other wasip1-only
  files (`*_wasip1.go`) leave out terminal UIs and commands that only work on a
  local project.
- **Build** (`build.sh`). Vendors this repository's source, overlays `patches/`
  (wasip1 stubs for third-party modules that don't support it: clipboard,
  moby/term, bubbletea, Prometheus tsdb/fileutil), and builds `gcx.wasm`
  (~160 MB) with standard Go.
- **Host** (this package). Runs `gcx.wasm` with WASI preview 1 plus the
  `gcx_http` module.

**The host never calls back into the guest.** In a Go wasip1 guest, entering a
`//go:wasmexport` function (such as a canonical-ABI `cabi_realloc`) while a host
import is still running lets the Go scheduler run other goroutines on the same
wasm stack. Results then reach the wrong caller, or the module traps. That's why
the guest polls and supplies its own buffers.

## Usage

```sh
experimental/sandbox/build.sh                            # → experimental/sandbox/gcx.wasm
(cd experimental/sandbox && go test ./...)               # end-to-end tests use it
```

```go
import "github.com/grafana/gcx/experimental/sandbox"

// Once, at startup: compile gcx (~1 s from a warm CacheDir, ~40 s cold).
rt, err := sandbox.New(ctx, gcxWasm, sandbox.Config{
	CacheDir:         cacheDir,
	MemoryLimitBytes: 512 << 20,             // per instance
	Transport:        http.DefaultTransport, // how the host makes allowed requests
})
defer rt.Close(ctx)

// Optional per-request policy, e.g. allow only reads.
readOnly := func(r *http.Request) error {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return fmt.Errorf("%s %s needs write access", r.Method, r.URL.Path)
	}
	return nil
}

// Per command: a fresh, isolated instance.
ctx, cancel := context.WithTimeout(ctx, 60*time.Second) // stops the guest
defer cancel()
res, err := rt.Run(ctx, sandbox.Invocation{
	Args: []string{"dashboards", "list"},
	Env: map[string]string{
		"GRAFANA_SERVER":        tenant.URL, // no credentials here
		"GCX_AGENT_MODE":        "true",     // machine-readable output
		"GCX_AGENT_SPILL_BYTES": "0",        // keep large results inline, not in temp files
	},
	Stdout: &out, Stderr: &errOut,
	Home:   "", // fresh temp $HOME, removed afterwards; or a per-tenant dir
	Egress: []sandbox.Destination{{
		Host:   tenant.Host,
		Header: http.Header{"Authorization": {"Bearer " + tenant.Token}},
	}},
	Authorize: readOnly,
})
// res.ExitCode is gcx's exit status. err is set for host failures and when ctx
// ends (it is then ctx.Err()).
```

A `Runtime` is safe for concurrent `Run`s; each run's policy and in-flight
requests are kept separate. With a precompiled module and a fresh instance per
call, `gcx version` takes about 80 ms (26 ms natively).

## Security model

Each `Run`:

- **Filesystem:** an empty read-only `/` and one writable `$HOME` at `/home`.
- **Environment:** only `Invocation.Env`.
- **Network:** no sockets. Every request goes to the host, which applies
  `Invocation.Egress`.
  - Only https requests to a listed host are sent. Matching is exact and
    case-insensitive, and a missing port means the scheme's default. Anything
    else fails in the guest with `egress denied`.
  - `Destination.AllowHTTP` opts a host into plain http, for local development
    only.
  - Each request is checked against its actual destination. A guest that
    overrides the `Host` header changes where the request goes, and egress checks
    that.
  - Redirect hops are separate requests and are checked again.
- **Per-request policy:** `Invocation.Authorize`, if set, judges every request
  that `Egress` allows, before credentials are added, e.g. by method and path.
  An error refuses the request, and the guest sees its text. Without
  `Authorize`, any method may reach an allowed host.
- **Credentials:** a destination's `Header` values are set by the host on every
  request to that host, replacing whatever the guest sent. They never follow a
  redirect to another host.
- **Memory:** the guest's is capped by `Config.MemoryLimitBytes`. gcx needs
  about 94 MiB, and `New` rejects anything lower. The host also buffers each
  response in full, outside that cap; to bound it, wrap `Config.Transport` in a
  round tripper that limits response size.
- **Time:** the guest stops when `ctx` is cancelled or its deadline passes.
  gcx's retry backoff sleeps can't be interrupted, so stopping can lag by up to
  one backoff interval.

## Limitations

- **Interactive features:** terminal UIs (prompts), the clipboard and mmap are
  stubbed out.
- **TLS:** handled by the host, so the guest's TLS settings (custom CA, mTLS)
  are ignored.
- **Buffering:** responses are buffered in full, so streaming and long-poll
  commands don't work.
- **Stack discovery:** gcx needs `GRAFANA_SERVER/bootdata` to succeed, so the
  stack's host must be in `Egress`.
- **Maintenance:** the `patches/` stubs need updating when gcx's dependencies
  change. CI builds the module on every relevant change, so a breakage shows up
  in the PR that causes it.
