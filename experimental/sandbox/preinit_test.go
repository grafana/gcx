package sandbox_test

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/grafana/gcx/experimental/sandbox"
)

// loadPreinit returns gcx-preinit.wasm, which build.sh writes next to
// gcx.wasm. GCX_SANDBOX_PREINIT_WASM overrides its path.
func loadPreinit(t *testing.T) []byte {
	t.Helper()
	if testing.Short() {
		t.Skip("end-to-end test; skipped with -short")
	}
	if path := os.Getenv("GCX_SANDBOX_PREINIT_WASM"); path != "" {
		wasm, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("GCX_SANDBOX_PREINIT_WASM: %v", err)
		}
		return wasm
	}
	wasm, err := os.ReadFile("gcx-preinit.wasm")
	if err != nil {
		t.Skipf("pre-initialized gcx module not built (%v); run ./build.sh", err)
	}
	return wasm
}

// A command must behave the same from the pre-initialized module as from
// gcx.wasm: everything init read from the host is reloaded for it.
func TestPreinitMatchesCommandModule(t *testing.T) {
	srv := newFakeGrafana(t, "a")
	command := newRuntime(t, sandbox.Config{Transport: multiTransport(srv)})
	preinit := newRuntimeFor(t, loadPreinit(t), sandbox.Config{Transport: multiTransport(srv)})

	home := fstest.MapFS{".config/gcx/config.yaml": {Data: []byte("contexts:\n  local:\n    grafana:\n      server: https://local.example\ncurrent-context: local\n")}}
	tests := []struct {
		name string
		args []string
		env  map[string]string
		home fstest.MapFS
	}{
		{name: "version in agent mode", args: []string{"version"}, env: map[string]string{"GCX_AGENT_MODE": "true"}},
		{name: "version for a person", args: []string{"version"}, env: map[string]string{"GCX_AGENT_MODE": "false"}},
		{name: "help tree", args: []string{"help-tree", "--depth", "1", "-o", "text"}, env: map[string]string{"GCX_AGENT_MODE": "true"}},
		{name: "help with NO_COLOR", args: []string{"slo", "--help"}, env: map[string]string{"GCX_AGENT_MODE": "false", "NO_COLOR": "1"}},
		{name: "help with forced color", args: []string{"slo", "--help"}, env: map[string]string{"GCX_AGENT_MODE": "false", "CLICOLOR_FORCE": "1"}},
		{name: "API request", args: []string{"api", "/api/health"}},
		{name: "large result in agent mode", args: []string{"api", "/big"}, env: map[string]string{"GCX_AGENT_MODE": "true"}},
		{name: "unknown command", args: []string{"nonesuch"}, env: map[string]string{"GCX_AGENT_MODE": "true"}},
		{name: "config from home", args: []string{"config", "view", "-o", "json"}, home: home},
		{name: "GC settings", args: []string{"version"}, env: map[string]string{"GOGC": "400", "GOMEMLIMIT": "384MiB"}},
		{name: "invalid GOGC", args: []string{"version"}, env: map[string]string{"GOGC": "lots"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			run := func(rt *sandbox.Runtime) (int, string, string) {
				inv, _ := invocation(srv, "secret", tc.args...)
				maps.Copy(inv.Env, tc.env)
				if tc.home != nil {
					inv.HomeFS = tc.home
				}
				var stdout, stderr bytes.Buffer
				inv.Stdout, inv.Stderr = &stdout, &stderr
				res, err := rt.Run(t.Context(), inv)
				if err != nil {
					t.Fatalf("Run: %v", err)
				}
				return res.ExitCode, stdout.String(), stderr.String()
			}
			wantCode, wantOut, wantErr := run(command)
			gotCode, gotOut, gotErr := run(preinit)
			if gotCode != wantCode {
				t.Errorf("exit %d, gcx.wasm exits %d", gotCode, wantCode)
			}
			if gotOut != wantOut {
				t.Errorf("stdout differs from gcx.wasm's\n got: %q\nwant: %q", gotOut, wantOut)
			}
			if gotErr != wantErr {
				t.Errorf("stderr differs from gcx.wasm's\n got: %q\nwant: %q", gotErr, wantErr)
			}
		})
	}
}

// The runtime's random state must differ per instance, though every instance
// starts from the same snapshot. The guest prints a hash under a seed made
// during init and one under a seed made after its reseed.
func TestPreinitReseedsRandomness(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and snapshots a guest; skipped with -short")
	}
	if _, err := exec.LookPath("wasmtime"); err != nil {
		t.Skip("needs wasmtime (mise install) to snapshot the guest")
	}
	rt := newRuntimeFor(t, buildPreinitGuest(t), sandbox.Config{MemoryLimitBytes: 64 << 20})
	// draw returns the hash under the seed made during init and the one
	// under a seed made after the reseed.
	draw := func() (string, string) {
		var out bytes.Buffer
		res, err := rt.Run(t.Context(), sandbox.Invocation{Stdout: &out, Stderr: &out})
		if err != nil || res.ExitCode != 0 {
			t.Fatalf("Run = %+v, %v; output:\n%s", res, err, out.String())
		}
		atInit, afterReseed, _ := strings.Cut(strings.TrimSpace(out.String()), " ")
		return atInit, afterReseed
	}
	initA, freshA := draw()
	initB, freshB := draw()
	// The seed made during init is the snapshot's, which shows both
	// instances really start from one snapshot.
	if initA != initB {
		t.Fatalf("hashes under the init seed differ (%s, %s): the instances don't share a snapshot", initA, initB)
	}
	if freshA == freshB {
		t.Errorf("two instances made the same seed after reseeding (hash %s)", freshA)
	}
}

// buildPreinitGuest builds testdata/preinitrand the way build.sh builds
// gcx-preinit.wasm: as a reactor with the runtime overlay, snapshotted by
// preinit.sh.
func buildPreinitGuest(t *testing.T) []byte {
	t.Helper()
	dir := t.TempDir()
	goroot, err := exec.CommandContext(t.Context(), "go", "env", "GOROOT").Output()
	if err != nil {
		t.Fatalf("go env GOROOT: %v", err)
	}
	overlay, err := json.Marshal(map[string]map[string]string{"Replace": {
		filepath.Join(strings.TrimSpace(string(goroot)), "src", "runtime", "gcx_preinit_wasip1.go"): mustAbs(t, "overlay/runtime/gcx_preinit_wasip1.go"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	overlayPath := filepath.Join(dir, "overlay.json")
	if err := os.WriteFile(overlayPath, overlay, 0o600); err != nil {
		t.Fatal(err)
	}
	reactor, snapshot := filepath.Join(dir, "reactor.wasm"), filepath.Join(dir, "snapshot.wasm")
	build := exec.CommandContext(t.Context(), "go", "build", "-buildmode=c-shared", "-overlay", overlayPath, "-o", reactor, "./testdata/preinitrand")
	build.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the guest: %v\n%s", err, out)
	}
	if out, err := exec.CommandContext(t.Context(), "./preinit.sh", reactor, snapshot).CombinedOutput(); err != nil {
		t.Fatalf("preinit.sh: %v\n%s", err, out)
	}
	wasm, err := os.ReadFile(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return wasm
}

func mustAbs(t *testing.T, path string) string {
	t.Helper()
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// newRuntimeFor is newRuntime for a given module.
func newRuntimeFor(t *testing.T, wasm []byte, cfg sandbox.Config) *sandbox.Runtime {
	t.Helper()
	cfg.CacheDir = compiledCache(t)
	rt, err := sandbox.New(context.Background(), wasm, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rt.Close(context.Background()) })
	return rt
}
