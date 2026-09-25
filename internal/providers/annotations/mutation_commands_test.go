package annotations_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/annotations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

type mutationLoader struct{ host string }

func (l mutationLoader) LoadGrafanaConfig(context.Context) (config.NamespacedRESTConfig, error) {
	return config.NamespacedRESTConfig{Config: rest.Config{Host: l.host}}, nil
}

func TestCreateCommandStructuredResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/annotations", r.URL.Path)
		_, _ = w.Write([]byte(`{"id":42}`))
	}))
	defer srv.Close()

	cmd := annotations.NewCreateCommandForTest(mutationLoader{host: srv.URL})
	cmd.SetIn(strings.NewReader(`{"text":"deploy"}`))
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"-f", "-", "-o", "json"})
	require.NoError(t, cmd.Execute())
	var result map[string]any
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
	require.Equal(t, "gcx.mutation", result["type"])
	require.Equal(t, "created", result["action"])
	target, ok := result["target"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "42", target["id"])
}

func TestDeleteCommandRequiresForceInAgentMode(t *testing.T) {
	t.Setenv("GCX_AGENT_MODE", "true")
	cmd := annotations.NewDeleteCommandForTest(mutationLoader{host: "http://127.0.0.1:1"})
	cmd.SetArgs([]string{"42"})
	err := cmd.Execute()
	require.ErrorContains(t, err, "--force")
}
