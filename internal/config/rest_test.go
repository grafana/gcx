package config_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	authlib "github.com/grafana/authlib/types"
	"github.com/grafana/gcx/internal/config"
)

func TestNewNamespacedRESTConfig_UsesBootdataStack(t *testing.T) {
	bootdataServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"settings": map[string]any{
				"namespace": "stacks-98765",
			},
		})
	}))
	defer bootdataServer.Close()

	ctx := config.Context{
		Grafana: &config.GrafanaConfig{
			Server:  bootdataServer.URL + "/grafana",
			StackID: 12345,
		},
	}

	restCfg := config.NewNamespacedRESTConfig(t.Context(), ctx)

	if got, want := restCfg.Namespace, authlib.CloudNamespaceFormatter(98765); got != want {
		t.Fatalf("expected namespace %s, got %s", want, got)
	}

	if ctx.Grafana.StackID != 12345 {
		t.Fatalf("expected original stack ID to remain unchanged, got %d", ctx.Grafana.StackID)
	}
}

func TestNewNamespacedRESTConfig_FallsBackOnBootdataError(t *testing.T) {
	bootdataServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer bootdataServer.Close()

	ctx := config.Context{
		Grafana: &config.GrafanaConfig{
			Server:  bootdataServer.URL,
			StackID: 555,
		},
	}

	restCfg := config.NewNamespacedRESTConfig(t.Context(), ctx)

	if got, want := restCfg.Namespace, authlib.CloudNamespaceFormatter(555); got != want {
		t.Fatalf("expected namespace %s, got %s", want, got)
	}
}

func TestNewNamespacedRESTConfig_FallsBackWhenBootdataNotStack(t *testing.T) {
	bootdataServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"settings": map[string]any{
				"namespace": "grafana",
			},
		})
	}))
	defer bootdataServer.Close()

	ctx := config.Context{
		Grafana: &config.GrafanaConfig{
			Server:  bootdataServer.URL,
			StackID: 42,
		},
	}

	restCfg := config.NewNamespacedRESTConfig(t.Context(), ctx)

	if got, want := restCfg.Namespace, authlib.CloudNamespaceFormatter(42); got != want {
		t.Fatalf("expected namespace %s, got %s", want, got)
	}
}
