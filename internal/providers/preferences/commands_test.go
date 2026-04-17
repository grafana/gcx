package preferences //nolint:testpackage // Tests exercise unexported update path and table codec.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

// stubLoader implements GrafanaConfigLoader for tests.
type stubLoader struct {
	cfg config.NamespacedRESTConfig
	err error
}

func (l *stubLoader) LoadGrafanaConfig(_ context.Context) (config.NamespacedRESTConfig, error) {
	return l.cfg, l.err
}

func TestPreferencesTableCodec_Encode(t *testing.T) {
	codec := &PreferencesTableCodec{}
	prefs := &OrgPreferences{
		Theme:           "dark",
		Timezone:        "UTC",
		WeekStart:       "monday",
		Locale:          "en-US",
		HomeDashboardID: 42,
	}

	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, prefs))

	out := buf.String()
	for _, want := range []string{"KEY", "VALUE", "theme", "dark", "timezone", "UTC", "weekStart", "monday", "locale", "en-US", "homeDashboardId", "42"} {
		assert.Contains(t, out, want)
	}
}

func TestPreferencesTableCodec_Format(t *testing.T) {
	assert.Equal(t, "table", string((&PreferencesTableCodec{}).Format()))
}

func TestPreferencesTableCodec_EncodeWrongType(t *testing.T) {
	codec := &PreferencesTableCodec{}
	var buf bytes.Buffer
	err := codec.Encode(&buf, "not-a-prefs")
	require.Error(t, err)
}

func TestPreferencesTableCodec_DecodeNotSupported(t *testing.T) {
	codec := &PreferencesTableCodec{}
	err := codec.Decode(strings.NewReader(""), &OrgPreferences{})
	require.Error(t, err)
}

func TestUpdateOpts_Validate(t *testing.T) {
	require.Error(t, (&updateOpts{}).Validate())
	require.NoError(t, (&updateOpts{File: "x"}).Validate())
}

func TestReadInput_File(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prefs.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"theme":"dark"}`), 0o600))

	data, err := readInput(strings.NewReader(""), path)
	require.NoError(t, err)
	assert.JSONEq(t, `{"theme":"dark"}`, string(data))
}

func TestReadInput_Stdin(t *testing.T) {
	data, err := readInput(strings.NewReader(`{"theme":"light"}`), "-")
	require.NoError(t, err)
	assert.JSONEq(t, `{"theme":"light"}`, string(data))
}

func TestReadInput_FileMissing(t *testing.T) {
	_, err := readInput(strings.NewReader(""), "/does/not/exist.json")
	require.Error(t, err)
}

func TestUpdateCommand_File(t *testing.T) {
	var received OrgPreferences
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, "/api/org/preferences", r.URL.Path)
		body, err := io.ReadAll(r.Body)
		if !assert.NoError(t, err) {
			return
		}
		if !assert.NoError(t, json.Unmarshal(body, &received)) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":"Preferences updated"}`))
	}))
	defer server.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "prefs.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"theme":"light","timezone":"UTC"}`), 0o600))

	loader := &stubLoader{cfg: config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}}}
	cmd := newUpdateCommand(loader)
	cmd.SetContext(t.Context())

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Flags().Set("file", path))
	require.NoError(t, cmd.RunE(cmd, nil))

	assert.Equal(t, "light", received.Theme)
	assert.Equal(t, "UTC", received.Timezone)
	assert.Contains(t, out.String(), "Organization preferences updated")
}

func TestUpdateCommand_RequiresFile(t *testing.T) {
	loader := &stubLoader{}
	cmd := newUpdateCommand(loader)
	cmd.SetContext(t.Context())
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.RunE(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--file")
}
