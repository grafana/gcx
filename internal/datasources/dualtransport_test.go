package datasources_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/datasources"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

const (
	// nsCollection is the app-platform collection path for the prometheus plugin
	// group in the "stacks-1" namespace used by these tests.
	nsCollection = "/apis/prometheus.datasource.grafana.app/v0alpha1/namespaces/stacks-1/datasources"

	// apisDiscovery is a minimal /apis APIGroupList that serves the prometheus
	// datasource group (plus an unrelated group that must be ignored).
	apisDiscovery = `{"kind":"APIGroupList","groups":[` +
		`{"name":"prometheus.datasource.grafana.app"},` +
		`{"name":"query.grafana.app"}]}`
)

func newDualTransport(t *testing.T, handler http.Handler) datasources.Transport {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	cfg := config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}, Namespace: "stacks-1"}
	tr, err := datasources.NewTransport(cfg)
	require.NoError(t, err)
	return tr
}

// TestDualCreatePrefersAppPlatform asserts a create routes to the per-plugin
// /apis collection with the correct K8s body shape, and never touches REST.
func TestDualCreatePrefersAppPlatform(t *testing.T) {
	var gotBody struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
		Metadata   struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"metadata"`
		Spec   map[string]any `json:"spec"`
		Secure map[string]struct {
			Create string `json:"create"`
		} `json:"secure"`
	}
	tr := newDualTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/apis":
			_, _ = w.Write([]byte(apisDiscovery))
		case r.Method == http.MethodPost && r.URL.Path == nsCollection:
			if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
				t.Errorf("decode create body: %v", err)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"apiVersion":"prometheus.datasource.grafana.app/v0alpha1","kind":"DataSource","metadata":{"name":"my-prom","resourceVersion":"1"},"spec":{"title":"My Prom","access":"proxy"}}`))
		case strings.HasPrefix(r.URL.Path, "/api/datasources"):
			t.Errorf("legacy REST must not be called: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))

	ds := &datasources.Datasource{
		UID: "my-prom", Name: "My Prom", Type: "prometheus", Access: "proxy",
		SecureJSONData: map[string]string{"basicAuthPassword": "s3cret"},
	}
	out, err := tr.Create(context.Background(), ds)
	require.NoError(t, err)
	assert.Equal(t, "my-prom", out.UID)
	assert.Equal(t, "prometheus", out.Type)

	assert.Equal(t, "prometheus.datasource.grafana.app/v0alpha1", gotBody.APIVersion)
	assert.Equal(t, "DataSource", gotBody.Kind)
	assert.Equal(t, "my-prom", gotBody.Metadata.Name)
	assert.Equal(t, "stacks-1", gotBody.Metadata.Namespace)
	_, hasType := gotBody.Spec["type"]
	assert.False(t, hasType, "spec must not carry the routing-only type field")
	assert.Equal(t, "s3cret", gotBody.Secure["basicAuthPassword"].Create)
}

// TestDualCreateFallsBackToREST asserts that when /apis discovery reports no
// datasource groups, create transparently uses the legacy REST API.
func TestDualCreateFallsBackToREST(t *testing.T) {
	restCalled := false
	tr := newDualTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/apis":
			http.NotFound(w, r)
		case r.Method == http.MethodPost && r.URL.Path == "/api/datasources":
			restCalled = true
			_, _ = w.Write([]byte(`{"datasource":{"uid":"my-prom","name":"My Prom","type":"prometheus"}}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))

	out, err := tr.Create(context.Background(), &datasources.Datasource{UID: "my-prom", Name: "My Prom", Type: "prometheus"})
	require.NoError(t, err)
	require.True(t, restCalled, "expected fallback to legacy REST create")
	assert.Equal(t, "my-prom", out.UID)
}

// TestDualHealthFallsBackToLegacy covers the wbkprez-prod case: CRUD is served
// via /apis but the health subresource is not, so health uses legacy REST.
func TestDualHealthFallsBackToLegacy(t *testing.T) {
	legacyHealth := false
	tr := newDualTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/apis":
			_, _ = w.Write([]byte(apisDiscovery))
		case r.Method == http.MethodGet && r.URL.Path == nsCollection:
			_, _ = w.Write([]byte(`{"items":[{"apiVersion":"prometheus.datasource.grafana.app/v0alpha1","metadata":{"name":"my-prom"},"spec":{"title":"My Prom"}}]}`))
		case r.URL.Path == nsCollection+"/my-prom/health":
			http.NotFound(w, r) // subresource not registered on this stack
		case r.URL.Path == "/api/datasources/uid/my-prom/health":
			legacyHealth = true
			_, _ = w.Write([]byte(`{"status":"OK","message":"ok"}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))

	res, err := tr.Health(context.Background(), "my-prom")
	require.NoError(t, err)
	require.True(t, legacyHealth, "expected fallback to legacy health endpoint")
	assert.Equal(t, "OK", res.Status)
}

// TestDualUpdateSendsResourceVersion asserts the app-platform PUT carries the
// resourceVersion fetched from the current object (optimistic concurrency).
func TestDualUpdateSendsResourceVersion(t *testing.T) {
	var putBody struct {
		Metadata struct {
			ResourceVersion string `json:"resourceVersion"`
		} `json:"metadata"`
	}
	tr := newDualTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/apis":
			_, _ = w.Write([]byte(apisDiscovery))
		case r.Method == http.MethodGet && r.URL.Path == nsCollection+"/my-prom":
			_, _ = w.Write([]byte(`{"apiVersion":"prometheus.datasource.grafana.app/v0alpha1","metadata":{"name":"my-prom","resourceVersion":"7"},"spec":{"title":"old"}}`))
		case r.Method == http.MethodPut && r.URL.Path == nsCollection+"/my-prom":
			if err := json.NewDecoder(r.Body).Decode(&putBody); err != nil {
				t.Errorf("decode update body: %v", err)
			}
			_, _ = w.Write([]byte(`{"apiVersion":"prometheus.datasource.grafana.app/v0alpha1","metadata":{"name":"my-prom","resourceVersion":"8"},"spec":{"title":"new"}}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))

	_, err := tr.Update(context.Background(), "my-prom", &datasources.Datasource{UID: "my-prom", Name: "new", Type: "prometheus"})
	require.NoError(t, err)
	assert.Equal(t, "7", putBody.Metadata.ResourceVersion)
}

// TestDualUpdateConflictSurfacesError asserts a 409 (stale resourceVersion) is
// surfaced as a typed error rather than silently retried.
func TestDualUpdateConflictSurfacesError(t *testing.T) {
	tr := newDualTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/apis":
			_, _ = w.Write([]byte(apisDiscovery))
		case r.Method == http.MethodGet && r.URL.Path == nsCollection+"/my-prom":
			_, _ = w.Write([]byte(`{"metadata":{"name":"my-prom","resourceVersion":"7"},"spec":{}}`))
		case r.Method == http.MethodPut && r.URL.Path == nsCollection+"/my-prom":
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"message":"the object has been modified"}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))

	_, err := tr.Update(context.Background(), "my-prom", &datasources.Datasource{UID: "my-prom", Name: "new", Type: "prometheus"})
	require.Error(t, err)
	var apiErr *datasources.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusConflict, apiErr.StatusCode)
}

// TestDualListAndGetViaAppPlatform asserts List aggregates the per-plugin group,
// reads never surface secret values, and GetByUID resolves via the built index.
func TestDualListAndGetViaAppPlatform(t *testing.T) {
	const item = `{"apiVersion":"prometheus.datasource.grafana.app/v0alpha1","metadata":{"name":"my-prom"},"spec":{"title":"My Prom","access":"proxy"},"secure":{"basicAuthPassword":{"name":"basicAuthPassword"}}}`
	tr := newDualTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/apis":
			_, _ = w.Write([]byte(apisDiscovery))
		case r.Method == http.MethodGet && r.URL.Path == nsCollection:
			_, _ = w.Write([]byte(`{"items":[` + item + `]}`))
		case r.Method == http.MethodGet && r.URL.Path == nsCollection+"/my-prom":
			_, _ = w.Write([]byte(item))
		case strings.HasPrefix(r.URL.Path, "/api/datasources"):
			t.Errorf("legacy REST must not be called: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))

	list, err := tr.List(context.Background())
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "my-prom", list[0].UID)
	assert.Equal(t, "prometheus", list[0].Type)
	assert.True(t, list[0].SecureJSONFields["basicAuthPassword"])
	assert.Empty(t, list[0].SecureJSONData, "reads must never surface secret values")

	got, err := tr.GetByUID(context.Background(), "my-prom")
	require.NoError(t, err)
	assert.Equal(t, "My Prom", got.Name)
}

// TestDualListForbiddenGroupFallsBackToREST asserts that when a served plugin
// group is not accessible (403), the app-platform list is treated as incomplete
// and List falls back to the permission-aware legacy REST list.
func TestDualListForbiddenGroupFallsBackToREST(t *testing.T) {
	const discovery = `{"kind":"APIGroupList","groups":[` +
		`{"name":"prometheus.datasource.grafana.app"},` +
		`{"name":"grafana-honeycomb-datasource.datasource.grafana.app"}]}`
	restListed := false
	tr := newDualTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/apis":
			_, _ = w.Write([]byte(discovery))
		case r.URL.Path == nsCollection:
			_, _ = w.Write([]byte(`{"items":[{"apiVersion":"prometheus.datasource.grafana.app/v0alpha1","metadata":{"name":"my-prom"},"spec":{}}]}`))
		case r.URL.Path == "/apis/grafana-honeycomb-datasource.datasource.grafana.app/v0alpha1/namespaces/stacks-1/datasources":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"kind":"Status","message":"access denied","code":403}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/datasources":
			restListed = true
			_, _ = w.Write([]byte(`[{"uid":"my-prom","name":"My Prom","type":"prometheus"},{"uid":"hc","name":"Honeycomb","type":"grafana-honeycomb-datasource"}]`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))

	list, err := tr.List(context.Background())
	require.NoError(t, err)
	require.True(t, restListed, "expected fallback to legacy REST list for a complete view")
	assert.Len(t, list, 2)
}

// TestDualGetNotServedFallsBackToREST asserts a not-found via the legacy
// fallback maps through IsNotFound when app-platform is not served.
func TestDualGetNotServedFallsBackToREST(t *testing.T) {
	tr := newDualTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/apis":
			http.NotFound(w, r)
		case r.Method == http.MethodGet && r.URL.Path == "/api/datasources/uid/missing":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Data source not found"}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))

	_, err := tr.GetByUID(context.Background(), "missing")
	require.Error(t, err)
	assert.True(t, datasources.IsNotFound(err))
}

// TestDualDeleteViaAppPlatform asserts delete resolves the group from the index
// and issues the app-platform DELETE.
func TestDualDeleteViaAppPlatform(t *testing.T) {
	deleted := false
	tr := newDualTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/apis":
			_, _ = w.Write([]byte(apisDiscovery))
		case r.Method == http.MethodGet && r.URL.Path == nsCollection:
			_, _ = w.Write([]byte(`{"items":[{"apiVersion":"prometheus.datasource.grafana.app/v0alpha1","metadata":{"name":"my-prom"},"spec":{}}]}`))
		case r.Method == http.MethodDelete && r.URL.Path == nsCollection+"/my-prom":
			deleted = true
			_, _ = w.Write([]byte(`{"kind":"Status","code":200}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))

	require.NoError(t, tr.Delete(context.Background(), "my-prom"))
	require.True(t, deleted, "expected app-platform delete")
}
