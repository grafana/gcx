package sandbox_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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
	path := os.Getenv("GCX_SANDBOX_WASM")
	if path == "" {
		path = "gcx.wasm"
	}
	wasm, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("gcx module not built (%v); run ./build.sh", err)
	}
	return wasm
}

func newRuntime(t *testing.T, cfg sandbox.Config) *sandbox.Runtime {
	t.Helper()
	cache, _ := os.UserCacheDir()
	cfg.CacheDir = filepath.Join(cache, "gcx-sandbox", "compiled")
	rt, err := sandbox.New(context.Background(), loadGCX(t), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rt.Close(context.Background()) })
	return rt
}

// fakeGrafana answers /bootdata with a stack namespace and everything else
// with a health body, recording each request's Authorization header, method
// and path. /slow never answers until the client gives up.
type fakeGrafana struct {
	*httptest.Server

	name  string
	mu    sync.Mutex
	auths []string
	reqs  []string // "METHOD /path"
}

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

	t.Run("RequireAccess", func(t *testing.T) {
		const post = "POST /api/dashboards/db"
		inv, out := invocation(d, "x", "api", "/api/dashboards/db", "-X", "POST", "-d", "{}")
		inv.Authorize = sandbox.RequireAccess(sandbox.AccessRead)
		res, err := rt.Run(ctx, inv)
		if err != nil {
			t.Fatal(err)
		}
		if res.ExitCode == 0 || !strings.Contains(out.String(), "access denied: "+post+" needs read-write access") {
			t.Errorf("exit %d, want failure logging access denied; output:\n%s", res.ExitCode, out)
		}
		if d.saw(post) {
			t.Error("refused POST reached the server")
		}

		inv, out = invocation(d, "x", "api", "/api/dashboards/db", "-X", "POST", "-d", "{}")
		inv.Authorize = sandbox.RequireAccess(sandbox.AccessWrite)
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

func TestMemoryLimit(t *testing.T) {
	// gcx declares a ~94 MiB minimum memory; a lower cap is rejected up front.
	cache, _ := os.UserCacheDir()
	_, err := sandbox.New(context.Background(), loadGCX(t), sandbox.Config{
		MemoryLimitBytes: 16 << 20,
		CacheDir:         filepath.Join(cache, "gcx-sandbox", "compiled"),
	})
	if err == nil || !strings.Contains(err.Error(), "over limit") {
		t.Fatalf("err %v, want memory limit rejection", err)
	}
	// A sufficient cap still runs gcx.
	rt := newRuntime(t, sandbox.Config{MemoryLimitBytes: 512 << 20})
	var out bytes.Buffer
	res, err := rt.Run(context.Background(), sandbox.Invocation{Args: []string{"version"}, Stdout: &out, Stderr: &out})
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("exit %d, err %v, output:\n%s", res.ExitCode, err, out.String())
	}
}
