package stacks_test

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/providers/stacks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStackTableCodec_Encode_Slice(t *testing.T) {
	out, err := stacks.ExportEncodeStackTable([]cloud.StackInfo{
		{Slug: "prod", Name: "Production", Status: "active", RegionSlug: "us", URL: "https://prod.grafana.net"},
		{Slug: "dev", Name: "Development", Status: "active", RegionSlug: "eu", URL: "https://dev.grafana.net"},
	}, false)

	require.NoError(t, err)
	assert.Contains(t, out, "SLUG")
	assert.Contains(t, out, "NAME")
	assert.Contains(t, out, "STATUS")
	assert.Contains(t, out, "REGION")
	assert.Contains(t, out, "URL")
	assert.Contains(t, out, "prod")
	assert.Contains(t, out, "dev")
	assert.NotContains(t, out, "PLAN", "narrow table should not include PLAN column")
}

func TestStackTableCodec_Encode_Wide(t *testing.T) {
	out, err := stacks.ExportEncodeStackTable([]cloud.StackInfo{
		{
			Slug: "prod", Name: "Production", Status: "active", RegionSlug: "us",
			URL: "https://prod.grafana.net", PlanName: "Pro", DeleteProtection: true,
			CreatedAt: "2024-01-15T10:30:00Z",
		},
	}, true)

	require.NoError(t, err)
	assert.Contains(t, out, "PLAN")
	assert.Contains(t, out, "DELETE-PROTECTION")
	assert.Contains(t, out, "CREATED")
	assert.Contains(t, out, "Pro")
	assert.Contains(t, out, "true")
	assert.Contains(t, out, "2024-01-15", "CreatedAt should be truncated to date")
}

func TestStackTableCodec_Encode_SingleStack(t *testing.T) {
	out, err := stacks.ExportEncodeStackTableSingle(cloud.StackInfo{
		Slug: "mystack", Name: "My Stack", Status: "active", RegionSlug: "us",
		URL: "https://mystack.grafana.net",
	})

	require.NoError(t, err)
	assert.Contains(t, out, "mystack")
	assert.Contains(t, out, "My Stack")
}

func TestStackTableCodec_Encode_InvalidType(t *testing.T) {
	var buf bytes.Buffer
	err := stacks.ExportStackTableCodec(false).Encode(&buf, "not a stack")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid data type")
}

func TestRegionTableCodec_Encode(t *testing.T) {
	out, err := stacks.ExportEncodeRegionTable([]cloud.Region{
		{Slug: "us", Name: "US Central", Description: "United States", Provider: "gcp", Status: "active"},
		{Slug: "eu", Name: "Belgium", Description: "Europe", Provider: "gcp", Status: "active"},
	})

	require.NoError(t, err)
	assert.Contains(t, out, "SLUG")
	assert.Contains(t, out, "PROVIDER")
	assert.Contains(t, out, "us")
	assert.Contains(t, out, "eu")
	assert.Contains(t, out, "gcp")
}

func TestRegionTableCodec_Encode_InvalidType(t *testing.T) {
	var buf bytes.Buffer
	err := stacks.ExportRegionTableCodec().Encode(&buf, "not regions")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid data type")
}

func TestDryRunSummary(t *testing.T) {
	var buf bytes.Buffer
	stacks.ExportDryRunSummary(&buf, "POST", "/api/instances", map[string]string{"name": "test"})

	out := buf.String()
	assert.Contains(t, out, "Dry run: POST /api/instances")
	assert.Contains(t, out, `"name"`)
	assert.Contains(t, out, `"test"`)
}

func TestDryRunSummary_NilBody(t *testing.T) {
	var buf bytes.Buffer
	stacks.ExportDryRunSummary(&buf, "DELETE", "/api/instances/mystack", nil)

	out := buf.String()
	assert.Contains(t, out, "Dry run: DELETE /api/instances/mystack")
	assert.NotContains(t, out, "{", "nil body should not produce JSON output")
}

func TestLabelsFromFlag(t *testing.T) {
	tests := []struct {
		name    string
		input   []string
		want    map[string]string
		wantErr string
	}{
		{name: "nil input", input: nil, want: nil},
		{name: "empty input", input: []string{}, want: nil},
		{name: "single label", input: []string{"env=prod"}, want: map[string]string{"env": "prod"}},
		{name: "multiple labels", input: []string{"env=prod", "team=platform"}, want: map[string]string{"env": "prod", "team": "platform"}},
		{name: "value with equals sign", input: []string{"config=key=value"}, want: map[string]string{"config": "key=value"}},
		{name: "empty value", input: []string{"flag="}, want: map[string]string{"flag": ""}},
		{name: "missing equals", input: []string{"noequalssign"}, wantErr: "invalid label"},
		{name: "empty key", input: []string{"=value"}, wantErr: "invalid label"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := stacks.ExportLabelsFromFlag(tt.input)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
