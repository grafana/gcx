package experiments //nolint:testpackage // Exercises the unexported command constructor and its output.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"k8s.io/client-go/rest"
)

type testLoader struct{ cfg config.NamespacedRESTConfig }

func (l testLoader) LoadGrafanaConfig(context.Context) (config.NamespacedRESTConfig, error) {
	return l.cfg, nil
}

func runList(t *testing.T, loader testLoader, args ...string) (string, string, error) {
	t.Helper()
	cmd := newListCommand(loader)
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

func TestList_UsesGrafanaAuthAndRetainsExperimentFields(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != listPath {
			t.Errorf("request = %s %s, want GET %s", r.Method, r.URL.Path, listPath)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q, want configured Grafana bearer token", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"items":[{"apiVersion":"odin.ext.grafana.com/v1alpha1","kind":"Experiment","metadata":{"name":"checkout-conversion","namespace":"default"},"spec":{"title":"Checkout conversion","status":"draft","description":"Keep this field"}}]}`)
	}))
	defer server.Close()
	loader := testLoader{cfg: config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL, BearerToken: "test-token"}}}

	stdout, _, err := runList(t, loader)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"NAME", "TITLE", "STATUS", "checkout-conversion", "Checkout conversion", "draft"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("table output %q does not contain %q", stdout, want)
		}
	}

	stdout, _, err = runList(t, loader, "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	var items []struct {
		Spec struct {
			Description string `json:"description"`
		} `json:"spec"`
	}
	if err := json.Unmarshal([]byte(stdout), &items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Spec.Description != "Keep this field" {
		t.Errorf("JSON output lost resource fields: %s", stdout)
	}
}

func TestList_ExplainsUnavailablePlugin(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	loader := testLoader{cfg: config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}}}

	_, _, err := runList(t, loader)
	if err == nil || !strings.Contains(err.Error(), "grafana-odin-app is enabled") {
		t.Fatalf("error = %v, want plugin availability guidance", err)
	}
}

func TestList_WarnsWhenPluginPageMayBeIncomplete(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"items":[`)
		for i := range pluginPageSize {
			if i > 0 {
				_, _ = fmt.Fprint(w, ",")
			}
			_, _ = fmt.Fprintf(w, `{"metadata":{"name":"exp-%d"},"spec":{"title":"Experiment","status":"draft"}}`, i)
		}
		_, _ = fmt.Fprint(w, `]}`)
	}))
	defer server.Close()
	loader := testLoader{cfg: config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}}}

	_, stderr, err := runList(t, loader, "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr, "may be incomplete") {
		t.Errorf("missing page-cap warning: %q", stderr)
	}
}
