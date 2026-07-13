package publicdashboards_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/publicdashboards"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubLoader implements RESTConfigLoader for tests without triggering real config loading.
type stubLoader struct{}

func (stubLoader) LoadGrafanaConfig(_ context.Context) (config.NamespacedRESTConfig, error) {
	return config.NamespacedRESTConfig{}, assert.AnError
}

func TestReadPublicDashboardSpec_FromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pd.json")
	payload := []byte(`{"isEnabled":true,"annotationsEnabled":true,"share":"public"}`)
	require.NoError(t, os.WriteFile(path, payload, 0o600))

	pd, err := publicdashboards.ReadPublicDashboardSpecForTest(path, nil)
	require.NoError(t, err)
	require.NotNil(t, pd)
	require.NotNil(t, pd.IsEnabled)
	assert.True(t, *pd.IsEnabled)
	require.NotNil(t, pd.AnnotationsEnabled)
	assert.True(t, *pd.AnnotationsEnabled)
	assert.Equal(t, "public", pd.Share)
}

func TestReadPublicDashboardSpec_FromStdin(t *testing.T) {
	payload := []byte(`{"isEnabled":false,"share":"public_with_email"}`)
	pd, err := publicdashboards.ReadPublicDashboardSpecForTest("-", bytes.NewReader(payload))
	require.NoError(t, err)
	require.NotNil(t, pd)
	require.NotNil(t, pd.IsEnabled)
	assert.False(t, *pd.IsEnabled)
	assert.Equal(t, "public_with_email", pd.Share)
}

// TestReadPublicDashboardSpec_PartialOmitsToggles proves the PATCH bug fix: a
// partial spec that omits the toggle fields yields nil pointers (not false), so
// marshaling the spec into a PATCH body omits them and the server preserves the
// existing values instead of silently disabling the dashboard.
func TestReadPublicDashboardSpec_PartialOmitsToggles(t *testing.T) {
	pd, err := publicdashboards.ReadPublicDashboardSpecForTest("-", bytes.NewReader([]byte(`{"share":"public"}`)))
	require.NoError(t, err)
	require.NotNil(t, pd)
	assert.Nil(t, pd.IsEnabled, "omitted isEnabled must stay nil, not default to false")
	assert.Nil(t, pd.AnnotationsEnabled)
	assert.Nil(t, pd.TimeSelectionEnabled)

	body, err := json.Marshal(pd)
	require.NoError(t, err)
	assert.NotContains(t, string(body), "isEnabled")
	assert.NotContains(t, string(body), "annotationsEnabled")
	assert.NotContains(t, string(body), "timeSelectionEnabled")
}

func TestReadPublicDashboardSpec_BadJSON(t *testing.T) {
	_, err := publicdashboards.ReadPublicDashboardSpecForTest("-", bytes.NewReader([]byte("not json")))
	require.Error(t, err)
}

func TestReadPublicDashboardSpec_FileMissing(t *testing.T) {
	_, err := publicdashboards.ReadPublicDashboardSpecForTest(filepath.Join(t.TempDir(), "missing.json"), nil)
	require.Error(t, err)
}

func TestCreateCommand_RequiredFlags(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantInError string
	}{
		{
			name:        "missing dashboard-uid",
			args:        []string{"-f", "pd.json"},
			wantInError: "dashboard-uid",
		},
		{
			name:        "missing file",
			args:        []string{"--dashboard-uid", "abc"},
			wantInError: "file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := publicdashboards.NewCreateCommandForTest(stubLoader{})
			cmd.SetArgs(tt.args)
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true

			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantInError)
		})
	}
}
