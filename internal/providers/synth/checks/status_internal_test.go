package checks

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"k8s.io/client-go/rest"
)

func TestSMMetricsDatasourceNameUsesSelectedRESTConfig(t *testing.T) {
	t.Parallel()

	var gotPath, gotAuthorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		gotAuthorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonData":{"metrics":{"grafanaName":"selected-prometheus"}}}`))
	}))
	t.Cleanup(server.Close)

	restCfg := config.NamespacedRESTConfig{Config: rest.Config{
		Host:        server.URL + "/selected-proxy",
		BearerToken: "selected-token",
	}}

	name, err := smMetricsDatasourceName(context.Background(), restCfg)
	if err != nil {
		t.Fatalf("smMetricsDatasourceName() error = %v", err)
	}
	if name != "selected-prometheus" {
		t.Fatalf("smMetricsDatasourceName() = %q, want %q", name, "selected-prometheus")
	}
	if gotPath != "/selected-proxy/api/plugins/grafana-synthetic-monitoring-app/settings" {
		t.Errorf("request path = %q, want selected REST-config path", gotPath)
	}
	if gotAuthorization != "Bearer selected-token" {
		t.Errorf("Authorization = %q, want selected REST-config bearer token", gotAuthorization)
	}
}
