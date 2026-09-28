package experiments //nolint:testpackage // Exercises the unexported command constructor and manifest reader.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"k8s.io/client-go/rest"
)

const createJSON = `{
  "apiVersion":"odin.ext.grafana.com/v1alpha1",
  "kind":"Experiment",
  "metadata":{"name":"checkout-conversion"},
  "spec":{
    "title":"Checkout conversion",
    "description":"Measure the updated checkout flow",
    "status":"draft",
    "grafanaFeatureToggle":{"name":"checkoutConversion"},
    "analyticsConfig":{
      "mode":"single",
      "unifiedQuery":{"datasourceUid":"analytics-events","datasourceType":"postgres","query":{"rawSql":"SELECT user_id, variant, conversion FROM events","nested":{"keep":true}}},
      "entityField":"user_id",
      "variantConfig":{"field":"variant","controlValues":["control"],"treatmentValues":["treatment"]},
      "metrics":[{"name":"Conversion","goodDirection":"up","valueField":"conversion","aggregation":"sum"}]
    }
  }
}`

const createYAML = `apiVersion: odin.ext.grafana.com/v1alpha1
kind: Experiment
metadata:
  name: checkout-conversion
spec:
  title: Checkout conversion
  description: Measure the updated checkout flow
  status: draft
  grafanaFeatureToggle:
    name: checkoutConversion
  analyticsConfig:
    mode: single
    unifiedQuery:
      datasourceUid: analytics-events
      datasourceType: postgres
      query:
        rawSql: SELECT user_id, variant, conversion FROM events
        nested:
          keep: true
    entityField: user_id
    variantConfig:
      field: variant
      controlValues: [control]
      treatmentValues: [treatment]
    metrics:
      - name: Conversion
        goodDirection: up
        valueField: conversion
        aggregation: sum
`

func runCreate(t *testing.T, loader testLoader, input string, args ...string) (string, error) {
	t.Helper()
	cmd := newCreateCommand(loader)
	var stdout bytes.Buffer
	cmd.SetIn(strings.NewReader(input))
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), err
}

func TestCreate_SubmitsCompleteManifestAndReturnsStoredResource(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		input string
		file  bool
	}{
		{name: "JSON from stdin", input: createJSON},
		{name: "YAML from file", input: createYAML, file: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != listPath {
					t.Errorf("request = %s %s, want POST %s", r.Method, r.URL.Path, listPath)
				}
				if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
					t.Errorf("Authorization = %q, want configured Grafana bearer token", got)
				}
				if got := r.Header.Get("Content-Type"); got != "application/json" {
					t.Errorf("Content-Type = %q, want application/json", got)
				}
				var submitted struct {
					Metadata struct {
						Name string `json:"name"`
					} `json:"metadata"`
					Spec struct {
						AnalyticsConfig struct {
							UnifiedQuery struct {
								Query map[string]any `json:"query"`
							} `json:"unifiedQuery"`
						} `json:"analyticsConfig"`
					} `json:"spec"`
				}
				if err := json.NewDecoder(r.Body).Decode(&submitted); err != nil {
					t.Error(err)
				}
				if submitted.Metadata.Name != "checkout-conversion" || submitted.Spec.AnalyticsConfig.UnifiedQuery.Query["rawSql"] != "SELECT user_id, variant, conversion FROM events" {
					t.Errorf("submitted manifest lost identity or query: %+v", submitted)
				}
				if nested, ok := submitted.Spec.AnalyticsConfig.UnifiedQuery.Query["nested"].(map[string]any); !ok || nested["keep"] != true {
					t.Errorf("submitted manifest lost datasource-specific fields: %+v", submitted)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				_, _ = fmt.Fprint(w, `{"apiVersion":"odin.ext.grafana.com/v1alpha1","kind":"Experiment","metadata":{"name":"checkout-conversion","namespace":"tenant-a"},"spec":{"title":"Checkout conversion","description":"Measure the updated checkout flow","status":"draft"}}`)
			}))
			defer server.Close()

			loader := testLoader{cfg: config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL, BearerToken: "test-token"}}}
			args := []string{"-f", "-", "-o", "json"}
			input := test.input
			if test.file {
				file := filepath.Join(t.TempDir(), "experiment.yaml")
				if err := os.WriteFile(file, []byte(input), 0o600); err != nil {
					t.Fatal(err)
				}
				args[1] = file
				input = ""
			}
			stdout, err := runCreate(t, loader, input, args...)
			if err != nil {
				t.Fatal(err)
			}
			var created Experiment
			if err := json.Unmarshal([]byte(stdout), &created); err != nil {
				t.Fatal(err)
			}
			if got := nestedString(created, "metadata", "namespace"); got != "tenant-a" {
				t.Errorf("output namespace = %q, want server-assigned tenant-a", got)
			}
		})
	}
}

func TestCreate_ExplainsServerErrors(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{name: "validation", status: http.StatusBadRequest, body: `{"message":"spec.analyticsConfig.variantConfig.field: must not be blank"}`, want: "spec.analyticsConfig.variantConfig.field"},
		{name: "conflict", status: http.StatusConflict, body: `{"message":"experiments checkout-conversion already exists"}`, want: "already exists"},
		{name: "plugin missing", status: http.StatusNotFound, body: "404 page not found", want: "grafana-odin-app is enabled"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				_, _ = fmt.Fprint(w, test.body)
			}))
			defer server.Close()
			loader := testLoader{cfg: config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}}}
			_, err := runCreate(t, loader, createJSON, "-f", "-")
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestReadManifest_RejectsInvalidInput(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty", input: "", want: "empty"},
		{name: "wrong kind", input: `{"apiVersion":"odin.ext.grafana.com/v1alpha1","kind":"Dashboard","metadata":{"name":"x"},"spec":{}}`, want: "kind Experiment"},
		{name: "missing name", input: `{"apiVersion":"odin.ext.grafana.com/v1alpha1","kind":"Experiment","spec":{}}`, want: "metadata.name"},
		{name: "copied status", input: `{"apiVersion":"odin.ext.grafana.com/v1alpha1","kind":"Experiment","metadata":{"name":"x"},"spec":{},"status":{}}`, want: "top-level status"},
		{name: "duplicate YAML field", input: "apiVersion: odin.ext.grafana.com/v1alpha1\nkind: Experiment\nkind: Experiment\nmetadata:\n  name: x\nspec: {}\n", want: "already set"},
		{name: "multiple YAML documents", input: createYAML + "---\nkind: Dashboard\n", want: "multiple"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := readManifest("-", strings.NewReader(test.input))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestCreate_ExampleIsAUsableFullManifest(t *testing.T) {
	t.Parallel()
	stdout, err := runCreate(t, testLoader{}, "", "--example", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := readManifest("-", strings.NewReader(stdout)); err != nil {
		t.Fatalf("example cannot be submitted: %v", err)
	}
	var example struct {
		Spec struct {
			AnalyticsConfig struct {
				Metrics []json.RawMessage `json:"metrics"`
			} `json:"analyticsConfig"`
		} `json:"spec"`
	}
	if err := json.Unmarshal([]byte(stdout), &example); err != nil {
		t.Fatal(err)
	}
	if len(example.Spec.AnalyticsConfig.Metrics) == 0 {
		t.Fatal("example omits metrics")
	}
}
