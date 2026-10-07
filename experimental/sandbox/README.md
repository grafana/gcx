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
  own memory, args, env and filesystem view. The exception is a `Home` you pass
  to several calls, which they share (see `Invocation.Home`).
- **Nothing is reachable unless granted.** The guest sees an empty read-only `/`,
  one writable `$HOME`, only the environment variables it's given, and no sockets.
- **The host controls the network.** gcx hands every HTTP request to the host,
  which applies an egress policy and can add credentials the guest never sees.
- **Pure Go.** No cgo and no external runtime.

## How it works

- **gcx side** (`internal/httputils/wire_wasip1.go`). `httputils.WireTransport` is
  the innermost layer of every gcx HTTP client. In normal builds it does nothing.
  Under `GOOS=wasip1` it passes each request to the host's `gcx_http` import
  module, whose shape follows [wasi:http 0.3](https://github.com/WebAssembly/WASI/tree/main/proposals/http)
  flattened to core wasm (no component model):
  - `request_new` describes a request (method, scheme, authority, path, headers,
    body) and returns an ID; `handle` sends it
  - `poll` reports whether the response has arrived or the request failed
  - `get_status_code`, `get_headers` and `body_read` read the response; the
    body streams in chunks
  - `error_code` and `error_detail` describe a failure as a wasi:http `error-code`
  - `drop` abandons the request and frees the ID

  Requests from gcx's goroutines run concurrently on the host. Other wasip1-only
  files (`*_wasip1.go`) leave out terminal UIs and commands that only work on a
  local project.
- **Build** (`build.sh`). Vendors this repository's source, overlays `patches/`
  (wasip1 stubs for third-party modules that don't support it: clipboard,
  moby/term, bubbletea, Prometheus tsdb/fileutil, and a process-local file lock for gofrs/flock), and builds `gcx.wasm`
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
		"GRAFANA_SERVER": tenant.URL, // no credentials here
		"GCX_AGENT_MODE": "true",     // machine-readable output
	},
	Stdout: &out, Stderr: &errOut,
	Home:   "", // fresh temp $HOME, removed afterwards; or a per-tenant dir, shared by that tenant's runs
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

## Prebuilt images

Rather than building gcx and compiling it at startup, copy both from
`ghcr.io/grafana/gcx-wasm`. It's published by `.github/workflows/publish-gcx-wasm.yaml`
on every push to `main` and every `experimental/sandbox/v*` tag:

```dockerfile
FROM ghcr.io/grafana/gcx-wasm:sandbox-v0.2.0 AS gcx
# …
COPY --from=gcx / /usr/share/gcx/
```

```go
wasm, _ := os.ReadFile("/usr/share/gcx/gcx.wasm")
rt, err := sandbox.New(ctx, wasm, sandbox.Config{CacheDir: "/usr/share/gcx/cache"})
```

Each image holds `/gcx.wasm`, `/gcx.commit`, and `/cache/`, which is wazero's
compiled code for the module. With that cache, startup takes about 1 s instead
of about 40 s, and the cache can stay read-only.

- **Use the tag matching your version of this module.** A sandbox host runs only
  modules from the same gcx version, so pin `sandbox-vX.Y.Z` to match your
  `experimental/sandbox` version, or `sha-<commit>` for a pseudo-version. Don't
  use `main`.
- **Compiled code is per platform.** Images exist for `linux/amd64` and
  `linux/arm64`; Docker picks the one matching your build. wazero keys its cache
  on its own version, `GOARCH`/`GOOS`, the module bytes, the sandbox's fixed
  settings (termination on context cancellation, no listeners), and a few CPU
  features: SSE4.1, BMI1 and ABM on amd64, LSE atomics on arm64. Any modern
  server CPU has these. Embedder settings such as `MemoryLimitBytes` don't
  affect it. If anything differs (for example, your build resolves a different
  wazero version than this module pins), the cache misses and `New` compiles
  from scratch: slower, but correct.
- **Copy this module's wazero `replace`.** Until its memory fixes are released
  upstream, this module pins a wazero fork with a `replace` in its `go.mod`
  (see the comment there). Go ignores a dependency's `replace` directives, so
  put the same `require` and `replace` lines for `github.com/tetratelabs/wazero`
  in your own `go.mod`. Without them your build fails: the required version
  is the fork commit's pseudo-version, which upstream wazero doesn't have
  (`go get` reports `unknown revision`). Re-copy both lines whenever you
  update this module. A stale `replace` silently wins over the newer
  `require`, so your build runs the old fork commit while reporting the new
  version, and wazero keys its cache on that version: it would load compiled
  code from the published cache that a different fork commit produced.

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
  about 94 MiB, and `New` rejects anything lower. Outside that cap, the host
  holds at most a couple of 32 KiB chunks of each response body, which it
  streams to the guest as the guest reads it.
  On Linux, each instance's memory is its own mapping, reserving address
  space for the whole cap (4 GiB when unset) but touched only as the guest
  uses it, and returned to the OS when `Run` returns. If the kernel refuses
  the reservation, as under strict overcommit, that instance uses the Go heap
  and its memory is left for the GC, as on other platforms.
- **Time:** the guest stops when `ctx` is cancelled or its deadline passes.
  gcx's retry backoff sleeps can't be interrupted, so stopping can lag by up to
  one backoff interval.

## Limitations

- **Interactive features:** terminal UIs (prompts), the clipboard and mmap are
  stubbed out.
- **TLS:** handled by the host, so the guest's TLS settings (custom CA, mTLS)
  are ignored.
- **Request bodies:** sent in one piece, not streamed. Responses stream, except
  under `--insecure-log-http-payload`, which reads each body to its end first.
- **Stack discovery:** gcx needs `GRAFANA_SERVER/bootdata` to succeed, so the
  stack's host must be in `Egress`.
- **Maintenance:** the `patches/` stubs need updating when gcx's dependencies
  change. CI builds the module on every relevant change, so a breakage shows up
  in the PR that causes it.
