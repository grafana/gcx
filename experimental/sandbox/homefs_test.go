package sandbox_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/grafana/gcx/experimental/sandbox"
)

const homeFSDeviceID = "11111111-2222-4333-8444-555555555555"

func TestHomeFS(t *testing.T) {
	srv := newFakeGrafana(t, "a")
	rt := newRuntime(t, sandbox.Config{Transport: multiTransport(srv)})

	t.Run("serves a read-only home", func(t *testing.T) {
		inv, _ := invocation(srv, "tok", "api", "/api/health")
		var stdout, stderr bytes.Buffer
		inv.Stdout, inv.Stderr = &stdout, &stderr
		// Print the usage event that gcx would send instead of sending it, to
		// see the device ID gcx read from the seeded file.
		inv.Env["GCX_TELEMETRY"] = "log"
		inv.HomeFS = fstest.MapFS{
			".config/gcx/config.yaml":    {Data: []byte("contexts: {}\n")},
			".local/state/gcx/device-id": {Data: []byte(homeFSDeviceID + "\n")},
		}
		res, err := rt.Run(context.Background(), inv)
		if err != nil || res.ExitCode != 0 {
			t.Fatalf("exit %d, err %v, output:\n%s%s", res.ExitCode, err, &stdout, &stderr)
		}
		if !strings.Contains(stdout.String(), "fake") {
			t.Errorf("output missing server response:\n%s", &stdout)
		}
		for _, want := range []string{`"device_id":"` + homeFSDeviceID + `"`, `"device_id_persisted":true`} {
			if !strings.Contains(stderr.String(), want) {
				t.Errorf("usage event missing %s:\n%s", want, &stderr)
			}
		}
	})

	t.Run("rejects Home together with HomeFS", func(t *testing.T) {
		inv, _ := invocation(srv, "tok", "version")
		inv.Home = t.TempDir()
		inv.HomeFS = fstest.MapFS{}
		if _, err := rt.Run(context.Background(), inv); err == nil {
			t.Fatal("Run succeeded with both Home and HomeFS set")
		}
	})
}
