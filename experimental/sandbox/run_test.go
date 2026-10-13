package sandbox_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/grafana/gcx/experimental/sandbox"
)

// These tests run the real gcx module; build it first with ./build.sh.
// GCX_SANDBOX_WASM overrides its path.

func loadGCX(t *testing.T) []byte {
	t.Helper()
	if testing.Short() {
		t.Skip("end-to-end test; skipped with -short")
	}
	path := os.Getenv("GCX_SANDBOX_WASM")
	if path != "" { // CI sets it, so a missing module fails rather than skipping every test
		wasm, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("GCX_SANDBOX_WASM: %v", err)
		}
		return wasm
	}
	wasm, err := os.ReadFile("gcx.wasm")
	if err != nil {
		t.Skipf("gcx module not built (%v); run ./build.sh", err)
	}
	return wasm
}

// compiledCache is where the tests keep wazero's compiled code between runs.
func compiledCache(t *testing.T) string {
	t.Helper()
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(cache, "gcx-sandbox", "compiled")
}

func newRuntime(t *testing.T, cfg sandbox.Config) *sandbox.Runtime {
	t.Helper()
	cfg.CacheDir = compiledCache(t)
	rt, err := sandbox.New(context.Background(), loadGCX(t), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rt.Close(context.Background()) })
	return rt
}

// fakeGrafana answers /bootdata with a stack namespace and everything else
// with a health body, recording each request's Authorization header, method
// and path. /slow never answers until the client gives up; /big answers with
// bigBody.
type fakeGrafana struct {
	*httptest.Server

	name  string
	mu    sync.Mutex
	auths []string
	reqs  []string // "METHOD /path"
}

// bigBody is well over gcx's 100 KiB agent-mode spill threshold.
func bigBody() string { return strings.Repeat("x", 2<<20) }

func newFakeGrafana(t *testing.T, name string) *fakeGrafana {
	t.Helper()
	f := &fakeGrafana{name: name}
	f.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.auths = append(f.auths, r.Header.Get("Authorization"))
		f.reqs = append(f.reqs, r.Method+" "+r.URL.Path)
		f.mu.Unlock()
		switch r.URL.Path {
		case "/slow":
			<-r.Context().Done()
			return
		case "/echo":
			_, _ = io.Copy(w, r.Body)
			return
		case "/big":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":"` + bigBody() + `"}`))
			return
		case "/bootdata": // gcx discovers the stack namespace before anything else
			_, _ = w.Write([]byte(`{"settings":{"namespace":"stacks-12345"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"database":"ok","version":"fake"}`))
	}))
	t.Cleanup(f.Close)
	return f
}

// host is the name the guest uses for this server. gcx rejects IP-address
// server URLs, so each fake server gets a hostname; multiTransport dials the
// real listener.
func (f *fakeGrafana) host() string { return f.name + ".example" }

func (f *fakeGrafana) url() string { return "https://" + f.host() }

func (f *fakeGrafana) saw(req string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Contains(f.reqs, req)
}

func (f *fakeGrafana) sawOnly(t *testing.T, auth string) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.auths) == 0 {
		t.Fatal("server saw no requests")
	}
	for _, a := range f.auths {
		if a != auth {
			t.Errorf("server saw Authorization %q, want %q", a, auth)
		}
	}
}

// multiTransport sends requests for each fake server's hostname to its
// listener, trusting its certificate (issued for example.com).
func multiTransport(servers ...*fakeGrafana) http.RoundTripper {
	addrs := map[string]string{}
	for _, f := range servers {
		addrs[f.host()+":443"] = f.Listener.Addr().String()
	}
	base, ok := servers[0].Client().Transport.(*http.Transport)
	if !ok {
		panic("httptest client transport is not an *http.Transport")
	}
	t := base.Clone()
	t.TLSClientConfig.ServerName = "example.com"
	t.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		listener, ok := addrs[addr]
		if !ok {
			return nil, fmt.Errorf("unexpected dial to %s", addr)
		}
		return (&net.Dialer{}).DialContext(ctx, network, listener)
	}
	return t
}

func invocation(srv *fakeGrafana, token string, args ...string) (sandbox.Invocation, *bytes.Buffer) {
	var out bytes.Buffer
	return sandbox.Invocation{
		Args:   args,
		Env:    map[string]string{"GRAFANA_SERVER": srv.url()},
		Stdout: &out,
		Stderr: &out,
		Egress: []sandbox.Destination{{
			Host:   srv.host(),
			Header: http.Header{"Authorization": {"Bearer " + token}},
		}},
	}, &out
}

func TestRun(t *testing.T) {
	a, b, c := newFakeGrafana(t, "a"), newFakeGrafana(t, "b"), newFakeGrafana(t, "c")
	d := newFakeGrafana(t, "d")
	rt := newRuntime(t, sandbox.Config{Transport: multiTransport(a, b, c, d)})
	ctx := context.Background()

	t.Run("injects credentials", func(t *testing.T) {
		inv, out := invocation(a, "secret-a", "api", "/api/health")
		res, err := rt.Run(ctx, inv)
		if err != nil || res.ExitCode != 0 {
			t.Fatalf("exit %d, err %v, output:\n%s", res.ExitCode, err, out)
		}
		if !strings.Contains(out.String(), "fake") {
			t.Errorf("output missing server response:\n%s", out)
		}
		a.sawOnly(t, "Bearer secret-a")
	})

	t.Run("agent mode writes large results to stdout", func(t *testing.T) {
		// Without the GCX_AGENT_SPILL_BYTES=0 default, gcx would try to spill
		// this to a /tmp the guest doesn't have.
		var stdout, stderr bytes.Buffer
		inv, _ := invocation(a, "secret-a", "api", "/big")
		inv.Env["GCX_AGENT_MODE"] = "true"
		inv.Stdout, inv.Stderr = &stdout, &stderr
		res, err := rt.Run(ctx, inv)
		if err != nil || res.ExitCode != 0 {
			t.Fatalf("exit %d, err %v, stderr:\n%s", res.ExitCode, err, stderr.String())
		}
		if !json.Valid(stdout.Bytes()) || !strings.Contains(stdout.String(), bigBody()) {
			t.Errorf("stdout (%d bytes) is not the JSON body; stderr:\n%s", stdout.Len(), stderr.String())
		}
		if strings.Contains(stderr.String(), bigBody()[:64]) {
			t.Error("body leaked to stderr")
		}
	})

	t.Run("denies other destinations", func(t *testing.T) {
		inv, out := invocation(c, "x", "-vvv", "api", "/api/health")
		inv.Egress = []sandbox.Destination{{Host: "only.example"}}
		res, err := rt.Run(ctx, inv)
		if err != nil {
			t.Fatal(err)
		}
		if res.ExitCode == 0 || !strings.Contains(out.String(), "egress denied") {
			t.Errorf("exit %d, want failure logging egress denied; output:\n%s", res.ExitCode, out)
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if len(c.auths) != 0 {
			t.Errorf("denied server received %d requests", len(c.auths))
		}
	})

	t.Run("Authorize refuses requests before they are sent", func(t *testing.T) {
		const post = "POST /api/dashboards/db"
		readOnly := func(r *http.Request) error {
			if r.Method != http.MethodGet {
				return fmt.Errorf("%s %s is not a read", r.Method, r.URL.Path)
			}
			return nil
		}
		inv, out := invocation(d, "x", "api", "/api/dashboards/db", "-X", "POST", "-d", "{}")
		inv.Authorize = readOnly
		res, err := rt.Run(ctx, inv)
		if err != nil {
			t.Fatal(err)
		}
		if res.ExitCode == 0 || !strings.Contains(out.String(), "request refused: "+post+" is not a read") {
			t.Errorf("exit %d, want failure logging the refusal; output:\n%s", res.ExitCode, out)
		}
		if d.saw(post) {
			t.Error("refused POST reached the server")
		}

		inv, out = invocation(d, "x", "api", "/api/dashboards/db", "-X", "POST", "-d", "{}")
		inv.Authorize = nil // no policy: any method may reach an allowed host
		if res, err := rt.Run(ctx, inv); err != nil || res.ExitCode != 0 {
			t.Fatalf("exit %d, err %v, output:\n%s", res.ExitCode, err, out)
		}
		if !d.saw(post) {
			t.Error("allowed POST never reached the server")
		}
	})

	t.Run("concurrent runs are isolated", func(t *testing.T) {
		var wg sync.WaitGroup
		for _, c := range []struct {
			srv   *fakeGrafana
			token string
		}{{a, "secret-a"}, {b, "secret-b"}, {a, "secret-a"}, {b, "secret-b"}} {
			wg.Go(func() {
				inv, out := invocation(c.srv, c.token, "api", "/api/health")
				if res, err := rt.Run(ctx, inv); err != nil || res.ExitCode != 0 {
					t.Errorf("exit %d, err %v, output:\n%s", res.ExitCode, err, out)
				}
			})
		}
		wg.Wait()
		a.sawOnly(t, "Bearer secret-a")
		b.sawOnly(t, "Bearer secret-b")
	})

	t.Run("deadline stops the guest", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		inv, _ := invocation(a, "x", "api", "/slow")
		start := time.Now()
		_, err := rt.Run(ctx, inv)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("err %v, want deadline exceeded", err)
		}
		if d := time.Since(start); d > 10*time.Second {
			t.Errorf("took %v to stop", d)
		}
	})
}

// TestRunIO covers what an invocation passes in besides args and env.
func TestRunIO(t *testing.T) {
	a := newFakeGrafana(t, "a")
	rt := newRuntime(t, sandbox.Config{Transport: multiTransport(a)})

	t.Run("feeds stdin to the guest", func(t *testing.T) {
		inv, out := invocation(a, "secret-a", "api", "/echo", "-d", "@-")
		inv.Stdin = strings.NewReader(`{"from":"stdin"}`)
		res, err := rt.Run(context.Background(), inv)
		if err != nil || res.ExitCode != 0 {
			t.Fatalf("exit %d, err %v, output:\n%s", res.ExitCode, err, out)
		}
		if !strings.Contains(out.String(), `"from"`) || !a.saw("POST /echo") {
			t.Errorf("server didn't echo stdin back; output:\n%s", out)
		}
	})

	t.Run("a panicking Authorize fails the request, not the host", func(t *testing.T) {
		inv, out := invocation(a, "x", "-vvv", "api", "/api/health")
		inv.Authorize = func(*http.Request) error { panic("policy bug") }
		res, err := rt.Run(context.Background(), inv)
		if err != nil || res.ExitCode == 0 || !strings.Contains(out.String(), "host panic: policy bug") {
			t.Errorf("exit %d, err %v; want a failed command reporting the panic; output:\n%s", res.ExitCode, err, out)
		}
	})

	t.Run("a reused Home persists between runs", func(t *testing.T) {
		home := t.TempDir()
		inv, out := invocation(a, "x", "config", "set", "stacks.saved.grafana.server", "https://saved.example")
		inv.Home = home
		if res, err := rt.Run(context.Background(), inv); err != nil || res.ExitCode != 0 {
			t.Fatalf("set: exit %d, err %v, output:\n%s", res.ExitCode, err, out)
		}
		inv, out = invocation(a, "x", "config", "view")
		inv.Home = home
		if res, err := rt.Run(context.Background(), inv); err != nil || res.ExitCode != 0 {
			t.Fatalf("view: exit %d, err %v, output:\n%s", res.ExitCode, err, out)
		}
		if !strings.Contains(out.String(), "saved.example") {
			t.Errorf("second run didn't see the first run's config:\n%s", out)
		}
	})
}

func TestMemoryLimit(t *testing.T) {
	// gcx declares a ~94 MiB minimum memory; a lower cap is rejected up front.
	rt, err := sandbox.New(context.Background(), loadGCX(t), sandbox.Config{
		MemoryLimitBytes: 16 << 20,
		CacheDir:         compiledCache(t),
	})
	if err == nil {
		_ = rt.Close(context.Background())
		t.Fatal("New succeeded, want memory limit rejection")
	}
	// A sufficient cap still runs gcx.
	rt = newRuntime(t, sandbox.Config{MemoryLimitBytes: 512 << 20})
	var out bytes.Buffer
	res, err := rt.Run(context.Background(), sandbox.Invocation{Args: []string{"version"}, Stdout: &out, Stderr: &out})
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("exit %d, err %v, output:\n%s", res.ExitCode, err, out.String())
	}
}
