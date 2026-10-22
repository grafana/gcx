package synth_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/query/synth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

const (
	schemaPath   = "/public/plugins/synthetic-monitoring-datasource/schema/v0alpha1/query.types.json"
	settingsPath = "/api/plugins/grafana-synthetic-monitoring-app/settings"
)

func mustReadFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	require.NoError(t, err)
	return body
}

func newCatalogTestClient(t *testing.T, handler http.HandlerFunc) *synth.CatalogClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: srv.URL},
		Namespace: "default",
	}
	client, err := synth.NewCatalogClient(cfg)
	require.NoError(t, err)
	return client
}

func TestCatalog_ParsesRealFixture(t *testing.T) {
	fixture := mustReadFixture(t, "query.types.json")
	client := newCatalogTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, schemaPath, r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fixture)
	})

	result, err := client.Catalog(context.Background())
	require.NoError(t, err)
	require.NotNil(t, result)

	require.Len(t, result.QueryTypes, 2)

	assert.Equal(t, "probe_execution_rate", result.QueryTypes[0].Name)
	assert.Equal(t,
		"Rate of successful check executions per probe, summed across all checks in the tenant.",
		result.QueryTypes[0].Description)
	assert.Empty(t, result.QueryTypes[0].Required, "TenantWideQuery has no required parameters")

	assert.Equal(t, "checks_uptime", result.QueryTypes[1].Name)
	assert.ElementsMatch(t, []string{"job", "instance", "frequency"}, result.QueryTypes[1].Required)
	assert.Contains(t, string(result.QueryTypes[1].Schema), `"probe"`,
		"the full schema, including optional params, must be preserved for `queries get`")
}

func TestCatalog_EmptyItemsIsSuccessNotError(t *testing.T) {
	client := newCatalogTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"kind":"QueryTypeDefinitionList","apiVersion":"datasource.grafana.app/v0alpha1","items":[]}`))
	})

	result, err := client.Catalog(context.Background())
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.QueryTypes)
}

func TestCatalog_MalformedJSONIsError(t *testing.T) {
	client := newCatalogTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{not json`))
	})

	_, err := client.Catalog(context.Background())
	require.Error(t, err)
}

// TestCatalog_WrongKindIsError guards against a 200 response that happens to
// decode into the same shape (e.g. an empty JSON object, or an unrelated
// document with no "kind") being silently read as a catalog with zero query
// types -- indistinguishable from a tenant that genuinely has none.
func TestCatalog_WrongKindIsError(t *testing.T) {
	client := newCatalogTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	})

	_, err := client.Catalog(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "QueryTypeDefinitionList")
}

func TestCatalog_404WithVersionBelowMinimum(t *testing.T) {
	client := newCatalogTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case schemaPath:
			http.NotFound(w, r)
		case settingsPath:
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"info": map[string]any{"version": "1.61.0"},
			})
		default:
			http.NotFound(w, r)
		}
	})

	_, err := client.Catalog(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "1.62.0")
	assert.Contains(t, err.Error(), "1.61.0")
}

func TestCatalog_404WithAppNotInstalled(t *testing.T) {
	client := newCatalogTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	_, err := client.Catalog(context.Background())
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "no synthetic monitoring app installed")
}

func TestCatalog_404WithSettingsForbiddenIsNotMisreportedAsUninstalled(t *testing.T) {
	client := newCatalogTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case schemaPath:
			http.NotFound(w, r)
		case settingsPath:
			w.WriteHeader(http.StatusForbidden)
		default:
			http.NotFound(w, r)
		}
	})

	_, err := client.Catalog(context.Background())
	require.Error(t, err)
	assert.NotContains(t, strings.ToLower(err.Error()), "no synthetic monitoring app installed")
	assert.Contains(t, err.Error(), "403")
}

func TestCatalog_404WithVersionAboveMinimumDoesNotBlameVersion(t *testing.T) {
	client := newCatalogTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case schemaPath:
			http.NotFound(w, r)
		case settingsPath:
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"info": map[string]any{"version": "1.70.0"},
			})
		default:
			http.NotFound(w, r)
		}
	})

	_, err := client.Catalog(context.Background())
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "requires Synthetic Monitoring app v1.62.0 or later; this context has v1.70.0")
	assert.Contains(t, err.Error(), "1.70.0")
}

func TestCatalog_OversizedBodyIsRejected(t *testing.T) {
	client := newCatalogTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// One byte over the shared response cap.
		_, _ = w.Write(make([]byte, 50<<20+1))
	})

	_, err := client.Catalog(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds")
}
