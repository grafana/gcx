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
