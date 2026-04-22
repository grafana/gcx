package fleet_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/providers/fleet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Pipeline round-trip tests
// ---------------------------------------------------------------------------

func TestPipelineToResource_RoundTrip(t *testing.T) {
	original := fleet.Pipeline{
		ID:       "18155",
		Name:     "my-pipeline",
		Enabled:  new(true),
		Contents: "logging { level = \"info\" }",
		Matchers: []string{"env=prod", "region=us-east"},
		Metadata: map[string]any{"version": "1.0"},
	}

	res, err := fleet.PipelineToResource(original, "default")
	require.NoError(t, err)
	require.NotNil(t, res)

	assert.Equal(t, fleet.PipelineAPIVersion, res.Object.GetAPIVersion())
	assert.Equal(t, fleet.PipelineKind, res.Object.GetKind())
	assert.Equal(t, "my-pipeline-18155", res.Object.GetName(), "metadata.name should be slug-id")
	assert.Equal(t, "default", res.Object.GetNamespace())

	roundTripped, err := fleet.PipelineFromResource(res)
	require.NoError(t, err)
	require.NotNil(t, roundTripped)

	assert.Equal(t, original.ID, roundTripped.ID, "ID should survive round-trip via slug")
	assert.Equal(t, original.Name, roundTripped.Name)
	assert.Equal(t, original.Enabled, roundTripped.Enabled)
	assert.Equal(t, original.Contents, roundTripped.Contents)
	assert.Equal(t, original.Matchers, roundTripped.Matchers)
}

func TestCollectorToResource_RoundTrip(t *testing.T) {
	enabled := true
	now := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)

	original := fleet.Collector{
		ID:            "42001",
		Name:          "my-collector",
		CollectorType: "alloy",
		Enabled:       &enabled,
		RemoteAttributes: map[string]string{
			"env": "production",
		},
		LocalAttributes: map[string]string{
			"region": "us-east-1",
		},
		CreatedAt: &now,
	}

	res, err := fleet.CollectorToResource(original, "stack-123")
	require.NoError(t, err)
	require.NotNil(t, res)

	assert.Equal(t, fleet.CollectorAPIVersion, res.Object.GetAPIVersion())
	assert.Equal(t, fleet.CollectorKind, res.Object.GetKind())
	assert.Equal(t, "my-collector-42001", res.Object.GetName(), "metadata.name should be slug-id")
	assert.Equal(t, "stack-123", res.Object.GetNamespace())

	roundTripped, err := fleet.CollectorFromResource(res)
	require.NoError(t, err)
	require.NotNil(t, roundTripped)

	assert.Equal(t, original.ID, roundTripped.ID, "ID should survive round-trip via slug")
	assert.Equal(t, original.Name, roundTripped.Name)
	assert.Equal(t, original.CollectorType, roundTripped.CollectorType)
	require.NotNil(t, roundTripped.Enabled)
	assert.Equal(t, enabled, *roundTripped.Enabled)
	assert.Equal(t, original.RemoteAttributes, roundTripped.RemoteAttributes)
	assert.Equal(t, original.LocalAttributes, roundTripped.LocalAttributes)
}

func TestPipelineToResource_StripsID(t *testing.T) {
	p := fleet.Pipeline{
		ID:       "99999",
		Name:     "test-pipeline",
		Enabled:  new(true),
		Contents: "some contents",
	}

	res, err := fleet.PipelineToResource(p, "default")
	require.NoError(t, err)

	spec, ok := res.Object.Object["spec"].(map[string]any)
	require.True(t, ok, "spec should be a map")
	assert.NotContains(t, spec, "id", "ID should be stripped from spec")
	assert.Equal(t, "test-pipeline-99999", res.Object.GetName(), "metadata.name should be slug-id")
}

func TestCollectorToResource_StripsID(t *testing.T) {
	col := fleet.Collector{
		ID:            "88888",
		Name:          "test-collector",
		CollectorType: "alloy",
	}

	res, err := fleet.CollectorToResource(col, "default")
	require.NoError(t, err)

	spec, ok := res.Object.Object["spec"].(map[string]any)
	require.True(t, ok, "spec should be a map")
	assert.NotContains(t, spec, "id", "ID should be stripped from spec")
	assert.Equal(t, "test-collector-88888", res.Object.GetName(), "metadata.name should be slug-id")
}

// ---------------------------------------------------------------------------
// PipelineTableCodec tests
// ---------------------------------------------------------------------------

func TestPipelineTableCodec_Encode(t *testing.T) {
	pipelines := []fleet.Pipeline{
		{ID: "p-1", Name: "alpha", Enabled: new(true), Matchers: []string{"env=prod"}},
		{ID: "p-2", Name: "beta", Enabled: new(false), Matchers: nil},
	}

	tests := []struct {
		name       string
		codec      fleet.PipelineTableCodec
		wantHeader []string
		wantValues []string
	}{
		{
			name:       "standard format has ID/NAME/ENABLED",
			codec:      fleet.PipelineTableCodec{Wide: false},
			wantHeader: []string{"ID", "NAME", "ENABLED"},
			wantValues: []string{"p-1", "alpha", "true", "p-2", "beta", "false"},
		},
		{
			name:       "wide format adds MATCHERS",
			codec:      fleet.PipelineTableCodec{Wide: true},
			wantHeader: []string{"ID", "NAME", "ENABLED", "MATCHERS"},
			wantValues: []string{"p-1", "alpha", "true", "env=prod", "p-2", "beta", "false"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := tt.codec.Encode(&buf, pipelines)
			require.NoError(t, err)

			output := buf.String()
			for _, h := range tt.wantHeader {
				assert.Contains(t, output, h, "header %q should be present", h)
			}
			for _, v := range tt.wantValues {
				assert.Contains(t, output, v, "value %q should be present", v)
			}

			// Verify "-" for empty matchers in wide mode.
			if tt.codec.Wide {
				lines := strings.Split(strings.TrimSpace(output), "\n")
				require.GreaterOrEqual(t, len(lines), 3) // header + 2 data lines
				assert.Contains(t, lines[2], "-", "empty matchers should show as -")
			}
		})
	}
}

func TestPipelineTableCodec_WrongType(t *testing.T) {
	codec := fleet.PipelineTableCodec{}
	var buf bytes.Buffer
	err := codec.Encode(&buf, "not a pipeline slice")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid data type")
}

// ---------------------------------------------------------------------------
// CollectorTableCodec tests
// ---------------------------------------------------------------------------

func TestCollectorTableCodec_Encode(t *testing.T) {
	enabled := true
	createdAt := time.Date(2025, 3, 15, 14, 30, 0, 0, time.UTC)

	collectors := []fleet.Collector{
		{ID: "c-1", Name: "coll-1", CollectorType: "alloy", Enabled: &enabled, CreatedAt: &createdAt},
		{ID: "c-2", Name: "coll-2", CollectorType: "", Enabled: nil, CreatedAt: nil},
	}

	tests := []struct {
		name       string
		codec      fleet.CollectorTableCodec
		wantHeader []string
		wantValues []string
	}{
		{
			name:       "standard format has ID/NAME/TYPE/ENABLED",
			codec:      fleet.CollectorTableCodec{Wide: false},
			wantHeader: []string{"ID", "NAME", "TYPE", "ENABLED"},
			wantValues: []string{"c-1", "coll-1", "alloy", "true", "c-2", "coll-2"},
		},
		{
			name:       "wide format adds CREATED_AT",
			codec:      fleet.CollectorTableCodec{Wide: true},
			wantHeader: []string{"ID", "NAME", "TYPE", "ENABLED", "CREATED_AT"},
			wantValues: []string{"c-1", "coll-1", "alloy", "true", "2025-03-15 14:30"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := tt.codec.Encode(&buf, collectors)
			require.NoError(t, err)

			output := buf.String()
			for _, h := range tt.wantHeader {
				assert.Contains(t, output, h, "header %q should be present", h)
			}
			for _, v := range tt.wantValues {
				assert.Contains(t, output, v, "value %q should be present", v)
			}

			// Verify nil enabled renders as "-".
			lines := strings.Split(strings.TrimSpace(output), "\n")
			require.GreaterOrEqual(t, len(lines), 3)
			// Second data line (c-2) should have "-" for type and enabled.
			assert.Contains(t, lines[2], "-")
		})
	}
}

func TestCollectorTableCodec_WrongType(t *testing.T) {
	codec := fleet.CollectorTableCodec{}
	var buf bytes.Buffer
	err := codec.Encode(&buf, "not a collector slice")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid data type")
}

// ---------------------------------------------------------------------------
// Pipeline protection guard tests
// ---------------------------------------------------------------------------

func TestIsManagedPipeline(t *testing.T) {
	tests := []struct {
		name     string
		pipeline string
		want     bool
	}{
		{
			name:     "beyla prefix is managed",
			pipeline: "beyla_k8s_appo11y_prod-1",
			want:     true,
		},
		{
			name:     "beyla prefix with different cluster suffix",
			pipeline: "beyla_k8s_appo11y_staging",
			want:     true,
		},
		{
			name:     "custom pipeline is not managed",
			pipeline: "my-custom-pipeline",
			want:     false,
		},
		{
			name:     "empty name is not managed",
			pipeline: "",
			want:     false,
		},
		{
			name:     "partial prefix match is not managed",
			pipeline: "beyla_k8s_",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fleet.IsManagedPipeline(tt.pipeline)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestPipelineProtectionGuard(t *testing.T) {
	tests := []struct {
		name            string
		pipelineName    string
		force           bool
		wantErr         bool
		wantErrContains string
	}{
		{
			name:            "blocks managed pipeline without force",
			pipelineName:    "beyla_k8s_appo11y_prod-1",
			force:           false,
			wantErr:         true,
			wantErrContains: "gcx instrumentation clusters update",
		},
		{
			name:         "allows managed pipeline with force",
			pipelineName: "beyla_k8s_appo11y_prod-1",
			force:        true,
			wantErr:      false,
		},
		{
			name:         "allows unmanaged pipeline without force",
			pipelineName: "my-custom-pipeline",
			force:        false,
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if err := json.NewEncoder(w).Encode(map[string]any{
					"id":   "123",
					"name": tt.pipelineName,
				}); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
				}
			}))
			defer server.Close()

			client := fleet.NewClient(context.Background(), server.URL, "inst", "token", true, nil)
			pipeline, err := client.GetPipeline(context.Background(), "123")
			require.NoError(t, err)
			require.NotNil(t, pipeline)

			// Apply the same guard logic as the commands.
			var guardErr error
			if !tt.force && fleet.IsManagedPipeline(pipeline.Name) {
				guardErr = fmt.Errorf("pipeline %q is managed by Grafana Cloud instrumentation; use 'gcx instrumentation clusters update <cluster> -f <file>' to modify instrumentation config, or pass --force to override", pipeline.Name)
			}

			if tt.wantErr {
				require.Error(t, guardErr)
				assert.Contains(t, guardErr.Error(), tt.wantErrContains)
				assert.Contains(t, guardErr.Error(), pipeline.Name)
			} else {
				require.NoError(t, guardErr)
			}
		})
	}
}
