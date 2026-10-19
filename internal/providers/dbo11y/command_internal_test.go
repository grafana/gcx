package dbo11y

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
)

// activationPath is where the product-activation resource lives for the
// context these tests build. The namespace is `default`: /bootdata answers
// 404, so stack discovery finds nothing to key it on.
const activationPath = "/apis/productactivation.ext.grafana.com/v1alpha1/namespaces/default/dbo11yconfigs/global"

// runCommand drives a leaf through cobra against a capture server, the way a
// user does, and reports stdout, stderr and the exit error. The helpers these
// tests used before stopped short of RunE, which is why both leaves shipped a
// path that wrote nothing to stdout before exiting non-zero.
func runCommand(t *testing.T, handler http.HandlerFunc, args ...string) (string, string, error) {
	t.Helper()

	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)

	f, createErr := os.CreateTemp(t.TempDir(), "gcx-dbo11y-config-*.yaml")
	if createErr != nil {
		t.Fatalf("CreateTemp() err = %v", createErr)
	}
	if _, writeErr := f.WriteString(`
contexts:
  default:
    grafana:
      server: "` + srv.URL + `"
      token: "test-token"
      org-id: 1
      tls:
        insecure-skip-verify: true
    datasources:
      prometheus: prom-uid
      loki: loki-uid
current-context: default
`); writeErr != nil {
		t.Fatalf("WriteString() err = %v", writeErr)
	}
	if closeErr := f.Close(); closeErr != nil {
		t.Fatalf("Close() err = %v", closeErr)
	}

	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(f.Name())

	root := &cobra.Command{Use: "test"}
	root.AddCommand(instancesCommand(loader))

	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs(args)

	return func() (string, string, error) {
		execErr := root.Execute()
		return out.String(), errOut.String(), execErr
	}()
}

// assertOneJSONValue checks the `finite` output contract: exactly one JSON
// value on stdout, however the command exited.
func assertOneJSONValue(t *testing.T, stdout string) {
	t.Helper()
	if stdout == "" {
		t.Fatal("stdout carried no JSON value")
	}
	dec := json.NewDecoder(bytes.NewReader([]byte(stdout)))
	var first json.RawMessage
	if decErr := dec.Decode(&first); decErr != nil {
		t.Fatalf("stdout is not a JSON value: %v\n---\n%s", decErr, stdout)
	}
	var extra json.RawMessage
	if decErr := dec.Decode(&extra); decErr == nil {
		t.Errorf("stdout carried more than one JSON value:\n%s", stdout)
	}
}

func TestInstancesList_NotActivatedStillEmitsOneDocument(t *testing.T) {
	// The API answers 404 when its routes are unavailable, and the activation
	// resource then confirms the product is switched off. The command exits
	// non-zero, but `instances list` is `finite`: a reporter handed an
	// EmittedError writes nothing further, so the document has to come from
	// here or `-o json` yields nothing at all.
	stdout, stderr, err := runCommand(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bootdata":
			http.Error(w, `{"message":"not a cloud stack"}`, http.StatusNotFound)
		case activationPath:
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"spec":{"enabled":false}}`)) //nolint:errcheck // test helper
		default:
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte("404 page not found\n")) //nolint:errcheck // test helper
		}
	}, "instances", "list", "-o", "json")

	if err == nil {
		t.Fatal("expected a non-zero exit when the product is not activated")
	}
	assertOneJSONValue(t, stdout)
	if !strings.Contains(stderr, "Database Observability") {
		t.Errorf("expected the activation hint on stderr, got %q", stderr)
	}
}

func TestInstancesList_EmptyInventoryEmitsOneDocument(t *testing.T) {
	stdout, _, err := runCommand(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/bootdata":
			http.Error(w, `{"message":"not a cloud stack"}`, http.StatusNotFound)
		case activationPath:
			w.Write([]byte(`{"spec":{"enabled":true}}`)) //nolint:errcheck // test helper
		default:
			w.Write([]byte(`{"status":"success","data":[]}`)) //nolint:errcheck // test helper
		}
	}, "instances", "list", "-o", "json")
	if err != nil {
		t.Fatalf("an empty inventory is not an error: %v", err)
	}
	assertOneJSONValue(t, stdout)
}

func TestInstancesGet_NotFoundStillEmitsOneDocument(t *testing.T) {
	// Same contract on the other leaf: the argument matches nothing, the
	// command exits 1, and stdout still carries its result document.
	stdout, stderr, err := runCommand(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/bootdata":
			http.Error(w, `{"message":"not a cloud stack"}`, http.StatusNotFound)
		case activationPath:
			// Activated, so the miss is the argument's fault, not the stack's.
			w.Write([]byte(`{"spec":{"enabled":true}}`)) //nolint:errcheck // test helper
		case apiBasePath + instancesPath:
			w.Write([]byte(`{"status":"success","data":[]}`)) //nolint:errcheck // test helper
		default:
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"status":"error","error":{"code":"instance_not_found","message":"instance not found"}}`)) //nolint:errcheck // test helper
		}
	}, "instances", "get", "nosuchthing", "-o", "json")

	if err == nil {
		t.Fatal("expected a non-zero exit for an instance that does not exist")
	}
	assertOneJSONValue(t, stdout)
	if !strings.Contains(stderr, "nosuchthing") {
		t.Errorf("expected the hint to name the argument, got %q", stderr)
	}
}

func TestInstancesList_RejectsDatasourceAsAFilter(t *testing.T) {
	// `datasource` is stripped before the filters go on the wire, so
	// accepting it would answer with the whole inventory while the user
	// believes it was applied.
	_, _, err := runCommand(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("expected the filter to be rejected before any request")
	}, "instances", "list", "--filter", "datasource=someone-elses-uid")

	if err == nil {
		t.Fatal("expected a usage error")
	}
	if !strings.Contains(err.Error(), "--datasource") {
		t.Errorf("error should point at the flag, got %v", err)
	}
}

// Health is the instance's presence in the API, so the two paths that emit a
// result document have to disagree about it — otherwise the field says nothing.
func TestInstancesGet_HealthTracksPresence(t *testing.T) {
	t.Run("an instance the API returns is reporting", func(t *testing.T) {
		stdout, _, err := runCommand(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/bootdata":
				http.Error(w, `{"message":"not a cloud stack"}`, http.StatusNotFound)
			case apiBasePath + instancePath("srv-1"):
				w.Write([]byte(`{"status":"success","data":{"server_id":"srv-1","instance":"db-1","engine":"postgres"}}`)) //nolint:errcheck // test helper
			case apiBasePath + queriesPath("srv-1"):
				w.Write([]byte(`{"status":"success","data":{"queries":[]}}`)) //nolint:errcheck // test helper
			case apiBasePath + sessionsPath("srv-1"):
				w.Write([]byte(`{"status":"success","data":{"server_id":"srv-1","engine":"postgres","total":25,"max_connections":429,"by_state":{"idle":16,"active":9},"longest_transaction_seconds":105.34}}`)) //nolint:errcheck // test helper
			case apiBasePath + waitEventsPath("srv-1"):
				w.Write([]byte(`{"status":"success","data":{"server_id":"srv-1","wait_events":[{"type":"Lock Wait","wait_seconds":12.5}],"total_wait_seconds":12.5}}`)) //nolint:errcheck // test helper
			default:
				w.Write([]byte(`{"status":"success","data":[]}`)) //nolint:errcheck // test helper
			}
		}, "instances", "get", "srv-1", "-o", "json")
		if err != nil {
			t.Fatalf("GetInstance: %v", err)
		}
		assertOneJSONValue(t, stdout)
		if !strings.Contains(stdout, `"up": true`) && !strings.Contains(stdout, `"up":true`) {
			t.Errorf("expected health.up true for an instance the API returned:\n%s", stdout)
		}
		for _, want := range []string{"max_connections", "longest_transaction_seconds", "Lock Wait"} {
			if !strings.Contains(stdout, want) {
				t.Errorf("expected %q in the result document:\n%s", want, stdout)
			}
		}
	})

	// Stacks upgrade on their own schedule, so a gcx that knows about /sessions
	// and /wait-events will meet plugins that do not serve them. An unmounted
	// route answers a bare text/plain 404 with no error code, which has to read
	// as "not available here" rather than failing the command — otherwise the
	// whole command breaks on every stack that is one release behind.
	t.Run("routes an older plugin does not serve are skipped, not fatal", func(t *testing.T) {
		stdout, _, err := runCommand(t, func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/bootdata":
				http.Error(w, `{"message":"not a cloud stack"}`, http.StatusNotFound)
			case apiBasePath + instancePath("srv-1"):
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"status":"success","data":{"server_id":"srv-1","instance":"db-1","engine":"postgres"}}`)) //nolint:errcheck // test helper
			case apiBasePath + queriesPath("srv-1"):
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"status":"success","data":{"queries":[]}}`)) //nolint:errcheck // test helper
			case apiBasePath + sessionsPath("srv-1"), apiBasePath + waitEventsPath("srv-1"):
				// What an unmounted route actually answers.
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte("404 page not found\n")) //nolint:errcheck // test helper
			default:
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"status":"success","data":[]}`)) //nolint:errcheck // test helper
			}
		}, "instances", "get", "srv-1", "-o", "json")

		if err != nil {
			t.Fatalf("an older plugin must not fail the command: %v", err)
		}
		assertOneJSONValue(t, stdout)
		for _, absent := range []string{"sessions", "wait_events"} {
			if strings.Contains(stdout, absent) {
				t.Errorf("expected %q to be omitted when the route is unmounted:\n%s", absent, stdout)
			}
		}
		// The rest of the document still stands.
		if !strings.Contains(stdout, `"up": true`) && !strings.Contains(stdout, `"up":true`) {
			t.Errorf("expected the instance still to be reported:\n%s", stdout)
		}
	})

	t.Run("an instance the API does not know is not reporting", func(t *testing.T) {
		stdout, _, err := runCommand(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/bootdata":
				http.Error(w, `{"message":"not a cloud stack"}`, http.StatusNotFound)
			case activationPath:
				// Activated, so the miss is the argument's fault, not the stack's.
				w.Write([]byte(`{"spec":{"enabled":true}}`)) //nolint:errcheck // test helper
			case apiBasePath + instancesPath:
				w.Write([]byte(`{"status":"success","data":[]}`)) //nolint:errcheck // test helper
			default:
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"status":"error","error":{"code":"instance_not_found","message":"instance not found"}}`)) //nolint:errcheck // test helper
			}
		}, "instances", "get", "nosuchthing", "-o", "json")

		if err == nil {
			t.Fatal("expected a non-zero exit for an instance the API does not know")
		}
		assertOneJSONValue(t, stdout)
		if !strings.Contains(stdout, `"up": false`) && !strings.Contains(stdout, `"up":false`) {
			t.Errorf("expected health.up false alongside the not-found document:\n%s", stdout)
		}
	})
}
