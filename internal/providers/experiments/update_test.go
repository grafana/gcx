package experiments //nolint:testpackage // Exercises the unexported command constructor and manifest reader.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"k8s.io/client-go/rest"
)

const updateManifest = `{"apiVersion":"odin.ext.grafana.com/v1alpha1","kind":"Experiment","metadata":{"name":"checkout-conversion","namespace":"tenant-a","uid":"uid-1","resourceVersion":"42"},"spec":{"title":"New title","description":"Keep details","status":"draft"}}`

func runUpdate(t *testing.T, loader testLoader, manifest string) (string, error) {
	t.Helper()
	return runUpdateArgs(t, loader, manifest, "checkout-conversion", "-f", "-", "-o", "json")
}

func runUpdateArgs(t *testing.T, loader testLoader, stdin string, args ...string) (string, error) {
	t.Helper()
	cmd := newUpdateCommand(loader)
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), err
}

func TestUpdate_FlagsFetchAndPreserveOtherFields(t *testing.T) {
	t.Parallel()
	current := strings.Replace(updateManifest, `"status":"draft"`, `"status":"draft","analyticsConfig":{"unifiedQuery":{"query":{"rawSql":"SELECT 1"}}}`, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != listPath+"/checkout-conversion" {
			t.Errorf("request path = %q", r.URL.Path)
		}
		switch r.Method {
		case http.MethodGet:
			_, _ = fmt.Fprint(w, current)
		case http.MethodPut:
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
			}
			var sent Experiment
			if err := json.Unmarshal(body, &sent); err != nil {
				t.Error(err)
			}
			if nestedString(sent, "spec", "title") != "Revised title" ||
				nestedString(sent, "spec", "description") != "Revised details" ||
				nestedString(sent, "spec", "status") != "ready_and_waiting" ||
				nestedString(sent, "metadata", "resourceVersion") != "42" ||
				nestedString(sent, "metadata", "uid") != "uid-1" ||
				!bytes.Contains(body, []byte("SELECT 1")) {
				t.Errorf("flag update lost existing fields or resource version: %s", body)
			}
			_, _ = fmt.Fprint(w, strings.Replace(string(body), `"resourceVersion":"42"`, `"resourceVersion":"43"`, 1))
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	}))
	defer server.Close()
	loader := testLoader{cfg: config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}}}

	stdout, err := runUpdateArgs(t, loader, "", "checkout-conversion", "--title", "Revised title", "--description", "Revised details", "--status", "ready_and_waiting", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	var updated Experiment
	if err := json.Unmarshal([]byte(stdout), &updated); err != nil {
		t.Fatal(err)
	}
	if got := nestedString(updated, "metadata", "resourceVersion"); got != "43" {
		t.Errorf("output resourceVersion = %q, want 43", got)
	}
}

func TestUpdate_UnchangedFlagSkipsPut(t *testing.T) {
	t.Parallel()
	var puts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			puts.Add(1)
		}
		_, _ = fmt.Fprint(w, updateManifest)
	}))
	defer server.Close()
	loader := testLoader{cfg: config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}}}

	_, err := runUpdateArgs(t, loader, "", "checkout-conversion", "--title", "New title")
	if err != nil {
		t.Fatal(err)
	}
	if got := puts.Load(); got != 0 {
		t.Errorf("PUT count = %d, want 0 for unchanged title", got)
	}
}

func TestUpdate_PutsCompleteExperimentWithResourceVersion(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != listPath+"/checkout-conversion" {
			t.Errorf("request = %s %s, want PUT %s/checkout-conversion", r.Method, r.URL.Path, listPath)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q, want configured Grafana bearer token", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		var sent Experiment
		if err := json.Unmarshal(body, &sent); err != nil {
			t.Error(err)
		}
		if nestedString(sent, "metadata", "resourceVersion") != "42" || nestedString(sent, "spec", "title") != "New title" {
			t.Errorf("PUT lost resource version or updated title: %s", body)
		}
		_, _ = fmt.Fprint(w, strings.Replace(updateManifest, `"resourceVersion":"42"`, `"resourceVersion":"43"`, 1))
	}))
	defer server.Close()
	loader := testLoader{cfg: config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL, BearerToken: "test-token"}}}

	stdout, err := runUpdate(t, loader, updateManifest)
	if err != nil {
		t.Fatal(err)
	}
	var updated Experiment
	if err := json.Unmarshal([]byte(stdout), &updated); err != nil {
		t.Fatal(err)
	}
	if got := nestedString(updated, "metadata", "resourceVersion"); got != "43" {
		t.Errorf("output resourceVersion = %q, want 43", got)
	}
}

func TestUpdate_RejectsInvalidManifestBeforeRequest(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		manifest string
		want     string
	}{
		{name: "wrong name", manifest: strings.Replace(updateManifest, `"name":"checkout-conversion"`, `"name":"other"`, 1), want: "does not match"},
		{name: "missing version", manifest: strings.Replace(updateManifest, `"resourceVersion":"42"`, `"resourceVersion":""`, 1), want: "metadata.resourceVersion"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := runUpdate(t, testLoader{}, test.manifest)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestUpdate_RejectsMixedFileAndFieldFlags(t *testing.T) {
	t.Parallel()
	_, err := runUpdateArgs(t, testLoader{}, updateManifest, "checkout-conversion", "-f", "-", "--title", "Revised title")
	if err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("error = %v, want conflicting input guidance", err)
	}
}

func TestUpdate_ReportsStaleResourceVersion(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = fmt.Fprint(w, `{"code":409,"error":"metadata.resourceVersion conflict; reload and retry"}`)
	}))
	defer server.Close()
	loader := testLoader{cfg: config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}}}

	_, err := runUpdate(t, loader, updateManifest)
	if err == nil || !strings.Contains(err.Error(), "HTTP 409: metadata.resourceVersion conflict; reload and retry") {
		t.Fatalf("error = %v, want Odin's version conflict", err)
	}
}
