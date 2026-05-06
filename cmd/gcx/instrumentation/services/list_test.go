package services_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/cmd/gcx/instrumentation/services"
	"github.com/grafana/gcx/internal/fleet"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/instrumentation"
	instrumout "github.com/grafana/gcx/internal/providers/instrumentation/output"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// discoveryTestServer serves fake RunK8sDiscovery responses.
type discoveryTestServer struct {
	items []map[string]any
}

func (s *discoveryTestServer) start(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/discovery.v1.DiscoveryService/RunK8sDiscovery", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := map[string]any{"items": s.items}
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func makeDiscoveryClient(t *testing.T, serverURL string) *instrumentation.Client {
	t.Helper()
	f := fleet.NewClient(context.Background(), serverURL, "inst-id", "api-token", true, nil)
	return instrumentation.NewClient(f)
}

func makeListOutOpts() *cmdio.Options {
	opts := &cmdio.Options{}
	opts.DefaultFormat("text")
	opts.RegisterCustomCodec("text", &instrumout.ServiceTableCodec{Wide: false})
	opts.RegisterCustomCodec("wide", &instrumout.ServiceTableCodec{Wide: true})
	// BindFlags initialises OutputFormat to the default ("text") via pflag default.
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	opts.BindFlags(fs)
	return opts
}

// TestRunList_StatusFilter_ERROR verifies --status=ERROR filters to only
// services in terminal-error state (INSTRUMENTATION_ERROR).
func TestRunList_StatusFilter_ERROR(t *testing.T) {
	ts := &discoveryTestServer{
		items: []map[string]any{
			{
				"clusterName":           "c1",
				"namespace":             "checkout",
				"name":                  "frontend",
				"instrumentationStatus": "INSTRUMENTED",
			},
			{
				"clusterName":           "c1",
				"namespace":             "checkout",
				"name":                  "payment",
				"instrumentationStatus": "INSTRUMENTATION_ERROR",
			},
			{
				"clusterName":           "c1",
				"namespace":             "checkout",
				"name":                  "cart",
				"instrumentationStatus": "NOT_INSTRUMENTED",
			},
		},
	}
	srv := ts.start(t)

	client := makeDiscoveryClient(t, srv.URL)
	outOpts := makeListOutOpts()
	require.NoError(t, outOpts.Validate())

	var buf bytes.Buffer
	err := services.RunList(
		context.Background(),
		&services.ListOpts{Status: "ERROR"},
		outOpts,
		client,
		instrumentation.PromHeaders{},
		&buf,
	)
	require.NoError(t, err)

	// Only "payment" (INSTRUMENTATION_ERROR) must appear in output.
	output := buf.String()
	assert.Contains(t, output, "payment")
	assert.NotContains(t, output, "frontend")
	assert.NotContains(t, output, "cart")
}

// TestRunList_StatusFilter_FullEnum verifies that full proto enum values also work.
func TestRunList_StatusFilter_FullEnum(t *testing.T) {
	ts := &discoveryTestServer{
		items: []map[string]any{
			{
				"clusterName":           "c1",
				"namespace":             "ns",
				"name":                  "svc-a",
				"instrumentationStatus": "INSTRUMENTED",
			},
			{
				"clusterName":           "c1",
				"namespace":             "ns",
				"name":                  "svc-b",
				"instrumentationStatus": "INSTRUMENTATION_ERROR",
			},
		},
	}
	srv := ts.start(t)

	client := makeDiscoveryClient(t, srv.URL)
	outOpts := makeListOutOpts()
	require.NoError(t, outOpts.Validate())

	var buf bytes.Buffer
	err := services.RunList(
		context.Background(),
		&services.ListOpts{Status: "INSTRUMENTATION_ERROR"},
		outOpts,
		client,
		instrumentation.PromHeaders{},
		&buf,
	)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "svc-b")
	assert.NotContains(t, output, "svc-a")
}

// TestRunList_ClusterFilter verifies that --cluster narrows to the specified cluster.
func TestRunList_ClusterFilter(t *testing.T) {
	ts := &discoveryTestServer{
		items: []map[string]any{
			{"clusterName": "c1", "namespace": "ns", "name": "svc-a", "instrumentationStatus": "INSTRUMENTED"},
			{"clusterName": "c2", "namespace": "ns", "name": "svc-b", "instrumentationStatus": "INSTRUMENTED"},
		},
	}
	srv := ts.start(t)

	client := makeDiscoveryClient(t, srv.URL)
	outOpts := makeListOutOpts()
	require.NoError(t, outOpts.Validate())

	var buf bytes.Buffer
	err := services.RunList(
		context.Background(),
		&services.ListOpts{Cluster: "c1"},
		outOpts,
		client,
		instrumentation.PromHeaders{},
		&buf,
	)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "svc-a")
	assert.NotContains(t, output, "svc-b")
}

// TestRunList_NamespaceFilter verifies that --namespace narrows to the specified namespace.
func TestRunList_NamespaceFilter(t *testing.T) {
	ts := &discoveryTestServer{
		items: []map[string]any{
			{"clusterName": "c1", "namespace": "checkout", "name": "svc-a", "instrumentationStatus": "INSTRUMENTED"},
			{"clusterName": "c1", "namespace": "payments", "name": "svc-b", "instrumentationStatus": "INSTRUMENTED"},
		},
	}
	srv := ts.start(t)

	client := makeDiscoveryClient(t, srv.URL)
	outOpts := makeListOutOpts()
	require.NoError(t, outOpts.Validate())

	var buf bytes.Buffer
	err := services.RunList(
		context.Background(),
		&services.ListOpts{Namespace: "checkout"},
		outOpts,
		client,
		instrumentation.PromHeaders{},
		&buf,
	)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "svc-a")
	assert.NotContains(t, output, "svc-b")
}

// TestRunList_EmptyResult verifies empty list outputs [] in JSON, never null.
func TestRunList_EmptyResult(t *testing.T) {
	ts := &discoveryTestServer{items: nil}
	srv := ts.start(t)

	client := makeDiscoveryClient(t, srv.URL)
	outOpts := &cmdio.Options{}
	outOpts.DefaultFormat("json")
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	outOpts.BindFlags(fs)
	require.NoError(t, outOpts.Validate())

	var buf bytes.Buffer
	err := services.RunList(
		context.Background(),
		&services.ListOpts{},
		outOpts,
		client,
		instrumentation.PromHeaders{},
		&buf,
	)
	require.NoError(t, err)

	// JSON output must be {"items":[]} not [] or null.
	assert.JSONEq(t, `{"items":[]}`, buf.String())
}

// TestRunList_JSONEnvelope_NonEmpty verifies non-empty JSON output is
// wrapped in {"items":[...]} (canonical list envelope).
func TestRunList_JSONEnvelope_NonEmpty(t *testing.T) {
	ts := &discoveryTestServer{
		items: []map[string]any{
			{"clusterName": "c1", "namespace": "ns", "name": "svc-a", "instrumentationStatus": "INSTRUMENTED"},
		},
	}
	srv := ts.start(t)

	client := makeDiscoveryClient(t, srv.URL)
	outOpts := &cmdio.Options{}
	outOpts.DefaultFormat("json")
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	outOpts.BindFlags(fs)
	require.NoError(t, outOpts.Validate())

	var buf bytes.Buffer
	err := services.RunList(
		context.Background(),
		&services.ListOpts{},
		outOpts,
		client,
		instrumentation.PromHeaders{},
		&buf,
	)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got), "output must be a JSON object")
	items, ok := got["items"]
	require.True(t, ok, "output must have 'items' key")
	itemsSlice, ok := items.([]any)
	require.True(t, ok, "'items' must be a JSON array")
	assert.Len(t, itemsSlice, 1)
}

// TestRunList_JSONFieldSelection_Valid verifies that --json with valid fields
// applies per-item projection and wraps the result in {"items":[...]}.
func TestRunList_JSONFieldSelection_Valid(t *testing.T) {
	ts := &discoveryTestServer{
		items: []map[string]any{
			{"clusterName": "c1", "namespace": "ns", "name": "svc-a", "instrumentationStatus": "INSTRUMENTED"},
		},
	}
	srv := ts.start(t)

	client := makeDiscoveryClient(t, srv.URL)
	outOpts := &cmdio.Options{}
	outOpts.DefaultFormat("json")
	outOpts.SetJSONFieldValidator(cmdio.MakeFieldValidator(instrumout.ServiceView{}))
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	outOpts.BindFlags(fs)
	// Set --json name,namespace
	require.NoError(t, fs.Set("json", "name,namespace"))
	require.NoError(t, outOpts.Validate())

	var buf bytes.Buffer
	err := services.RunList(
		context.Background(),
		&services.ListOpts{},
		outOpts,
		client,
		instrumentation.PromHeaders{},
		&buf,
	)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got), "output must be a JSON object; got: %s", buf.String())
	items, ok := got["items"]
	require.True(t, ok, "output must have 'items' key")
	itemsSlice, ok := items.([]any)
	require.True(t, ok)
	require.Len(t, itemsSlice, 1)
	firstItem, ok := itemsSlice[0].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, firstItem, "name")
	assert.Contains(t, firstItem, "namespace")
	assert.NotContains(t, firstItem, "clusterName")
}

// TestRunList_JSONFieldSelection_Unknown verifies --json with an
// unknown field name returns UnknownFieldSelectionError (which the converter
// maps to exit 2 + DetailedError).
func TestRunList_JSONFieldSelection_Unknown(t *testing.T) {
	ts := &discoveryTestServer{
		items: []map[string]any{
			{"clusterName": "c1", "namespace": "ns", "name": "svc-a"},
		},
	}
	srv := ts.start(t)

	client := makeDiscoveryClient(t, srv.URL)
	outOpts := &cmdio.Options{}
	outOpts.DefaultFormat("json")
	outOpts.SetJSONFieldValidator(cmdio.MakeFieldValidator(instrumout.ServiceView{}))
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	outOpts.BindFlags(fs)
	require.NoError(t, fs.Set("json", "bogus,name"))
	require.NoError(t, outOpts.Validate())

	var buf bytes.Buffer
	err := services.RunList(
		context.Background(),
		&services.ListOpts{},
		outOpts,
		client,
		instrumentation.PromHeaders{},
		&buf,
	)
	require.Error(t, err)
	var fieldErr cmdio.UnknownFieldSelectionError
	require.ErrorAs(t, err, &fieldErr, "error must be UnknownFieldSelectionError")
	assert.Contains(t, fieldErr.Fields, "bogus")
}

// noopConfigLoader satisfies fleet.ConfigLoader but never makes a network call.
// Used for tests that only exercise cobra.Args validation without RunE.
type noopConfigLoader struct{}

func (noopConfigLoader) LoadCloudConfig(_ context.Context) (providers.CloudRESTConfig, error) {
	return providers.CloudRESTConfig{}, errors.New("noop loader: not connected")
}

// TestListCmd_RejectsPositionalArgs verifies that cobra.NoArgs enforcement
// rejects any positional arguments passed to "services list".
func TestListCmd_RejectsPositionalArgs(t *testing.T) {
	tests := []struct {
		name            string
		args            []string
		wantErr         bool
		wantErrContains string
	}{
		{
			name:    "no positional args — cobra.NoArgs passes",
			args:    []string{},
			wantErr: false,
		},
		{
			name:            "one positional arg rejected",
			args:            []string{"prod-1"},
			wantErr:         true,
			wantErrContains: "unknown command",
		},
		{
			name:            "two positional args rejected",
			args:            []string{"prod-1", "checkout"},
			wantErr:         true,
			wantErrContains: "unknown command",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build the services parent command (noopConfigLoader — no live client needed).
			parentCmd := services.Command(noopConfigLoader{})
			// Navigate to the "list" subcommand.
			listCmd, _, findErr := parentCmd.Find([]string{"list"})
			require.NoError(t, findErr, "list subcommand must exist under services")
			require.NotNil(t, listCmd)

			// Call Args directly — exercises the cobra.NoArgs validator without RunE.
			argsErr := listCmd.Args(listCmd, tt.args)
			if tt.wantErr {
				require.Error(t, argsErr)
				assert.Contains(t, argsErr.Error(), tt.wantErrContains)
			} else {
				require.NoError(t, argsErr)
			}
		})
	}
}

// TestValidateWorkloadExists_SuggestionFlagForm verifies that the "workload not found"
// error suggestion uses flag form (--cluster=, --namespace=) not positional form.
func TestValidateWorkloadExists_SuggestionFlagForm(t *testing.T) {
	// Serve empty discovery results — workload will not be found.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
	}))
	defer srv.Close()

	client := makeDiscoveryClient(t, srv.URL)

	err := services.ValidateWorkloadExists(
		context.Background(),
		client,
		instrumentation.PromHeaders{},
		"prod-1", "checkout", "frontend",
	)
	require.Error(t, err)

	var de *fail.DetailedError
	require.ErrorAs(t, err, &de, "error must be *fail.DetailedError")
	require.NotEmpty(t, de.Suggestions, "Suggestions must be non-empty")

	// Every suggestion must use flag form, not positional form.
	for _, s := range de.Suggestions {
		assert.Contains(t, s, "--cluster=", "suggestion must use --cluster= form")
		assert.Contains(t, s, "--namespace=", "suggestion must use --namespace= form")
		assert.NotContains(t, s, "services list prod-1 checkout",
			"suggestion must not use old positional form")
	}
}

// TestNormalizeStatus verifies that short alias "ERROR" maps to StatusError.
func TestNormalizeStatus(t *testing.T) {
	tests := []struct {
		input string
		want  instrumentation.InstrumentationStatus
	}{
		{"ERROR", instrumentation.StatusError},
		{"error", instrumentation.StatusError},
		{"INSTRUMENTED", instrumentation.StatusInstrumented},
		{"instrumented", instrumentation.StatusInstrumented},
		{"PENDING_INSTRUMENTATION", instrumentation.StatusPendingInstrumentation},
		{"NOT_INSTRUMENTED", instrumentation.StatusNotInstrumented},
		{"EXCLUDED", instrumentation.StatusExcluded},
		// Full proto enum value passes through.
		{"INSTRUMENTATION_ERROR", instrumentation.StatusError},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := services.NormalizeStatus(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}
