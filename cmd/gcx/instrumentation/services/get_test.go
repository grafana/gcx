package services_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/cmd/gcx/instrumentation/services"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers/instrumentation"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunGet_HappyPath verifies that runGet returns the matching workload when found.
func TestRunGet_HappyPath(t *testing.T) {
	ts := &discoveryTestServer{
		items: []map[string]any{
			{
				"clusterName":           "prod-1",
				"namespace":             "checkout",
				"name":                  "frontend",
				"workloadType":          "Deployment",
				"instrumentationStatus": "INSTRUMENTED",
			},
			{
				"clusterName":           "prod-1",
				"namespace":             "checkout",
				"name":                  "payment",
				"instrumentationStatus": "NOT_INSTRUMENTED",
			},
		},
	}
	srv := ts.start(t)
	client := makeDiscoveryClient(t, srv.URL)

	outOpts := makeListOutOpts()
	require.NoError(t, outOpts.Validate())

	var buf bytes.Buffer
	err := services.RunGet(
		context.Background(),
		outOpts,
		client,
		"prod-1", "checkout", "frontend",
		instrumentation.PromHeaders{},
		&buf,
	)
	require.NoError(t, err)

	output := buf.String()
	// The requested workload must appear in output.
	assert.Contains(t, output, "frontend")
	// The other workload must NOT appear.
	assert.NotContains(t, output, "payment")
}

// TestRunGet_NotFound verifies that runGet returns a canonical *fail.DetailedError
// when the workload is not present in the discovery response.
func TestRunGet_NotFound(t *testing.T) {
	ts := &discoveryTestServer{
		items: []map[string]any{
			{
				"clusterName":           "prod-1",
				"namespace":             "checkout",
				"name":                  "payment",
				"instrumentationStatus": "INSTRUMENTED",
			},
		},
	}
	srv := ts.start(t)
	client := makeDiscoveryClient(t, srv.URL)

	outOpts := makeListOutOpts()
	require.NoError(t, outOpts.Validate())

	err := services.RunGet(
		context.Background(),
		outOpts,
		client,
		"prod-1", "checkout", "missing-svc",
		instrumentation.PromHeaders{},
		&bytes.Buffer{},
	)
	require.Error(t, err)

	// Legacy string checks — kept to lock in the rendered-string contract.
	assert.Contains(t, err.Error(), "missing-svc")
	assert.Contains(t, err.Error(), "not found")

	// Field-level assertions on the canonical *fail.DetailedError shape (AC-F1-2).
	var detailed *fail.DetailedError
	require.ErrorAs(t, err, &detailed, "error must be a *fail.DetailedError, got: %T", err)
	assert.Equal(t, "Resource not found", detailed.Summary)
	assert.Contains(t, detailed.Details, "missing-svc")
	assert.Contains(t, detailed.Details, "checkout")
	assert.Contains(t, detailed.Details, "prod-1")
	require.Len(t, detailed.Suggestions, 1)
	assert.Equal(t, "Run: gcx instrumentation services list --cluster=prod-1 --namespace=checkout", detailed.Suggestions[0])
	require.NotNil(t, detailed.ExitCode)
	assert.Equal(t, fail.ExitGeneralError, *detailed.ExitCode)
}

// TestRunGet_WrongCluster verifies that runGet returns not-found when the cluster
// matches but the service does not exist in that cluster.
func TestRunGet_WrongCluster(t *testing.T) {
	ts := &discoveryTestServer{
		items: []map[string]any{
			{
				"clusterName":           "prod-1",
				"namespace":             "checkout",
				"name":                  "frontend",
				"instrumentationStatus": "INSTRUMENTED",
			},
		},
	}
	srv := ts.start(t)
	client := makeDiscoveryClient(t, srv.URL)

	outOpts := makeListOutOpts()
	require.NoError(t, outOpts.Validate())

	err := services.RunGet(
		context.Background(),
		outOpts,
		client,
		"prod-2", "checkout", "frontend", // wrong cluster
		instrumentation.PromHeaders{},
		&bytes.Buffer{},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestRunGet_JSON_SingleObject verifies that JSON output is a single object {},
// not an array [{}] (J.1).
func TestRunGet_JSON_SingleObject(t *testing.T) {
	ts := &discoveryTestServer{
		items: []map[string]any{
			{
				"clusterName":           "prod-1",
				"namespace":             "checkout",
				"name":                  "frontend",
				"workloadType":          "Deployment",
				"instrumentationStatus": "INSTRUMENTED",
			},
		},
	}
	srv := ts.start(t)
	client := makeDiscoveryClient(t, srv.URL)

	// Build JSON-format output options.
	outOpts := &cmdio.Options{}
	outOpts.DefaultFormat("json")
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	outOpts.BindFlags(fs)
	require.NoError(t, outOpts.Validate())

	var buf bytes.Buffer
	err := services.RunGet(
		context.Background(),
		outOpts,
		client,
		"prod-1", "checkout", "frontend",
		instrumentation.PromHeaders{},
		&buf,
	)
	require.NoError(t, err)

	output := strings.TrimSpace(buf.String())
	// Must start with '{' (single object), not '[' (array).
	assert.True(t, strings.HasPrefix(output, "{"), "JSON output should be a single object, got: %s", output)
	// Must not wrap in an array.
	assert.False(t, strings.HasPrefix(output, "["), "JSON output must not be an array, got: %s", output)
	// The workload name must appear in the output.
	assert.Contains(t, output, "frontend")
}
