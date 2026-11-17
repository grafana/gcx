package config_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/config"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

func TestNewNamespacedRESTConfig_SurfacesAPIWarnings(t *testing.T) {
	tests := []struct {
		name      string
		agentMode bool
		want      string
	}{
		{
			name: "human output",
			want: "warn: folder title does not follow the naming convention\n" +
				"warn: folder.grafana.app/v1beta1 is deprecated\n",
		},
		{
			name:      "agent output",
			agentMode: true,
			want: `{"class":"warning","summary":"folder title does not follow the naming convention"}` + "\n" +
				`{"class":"warning","summary":"folder.grafana.app/v1beta1 is deprecated"}` + "\n",
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("Warning", `299 - "folder title does not follow the naming convention"`)
		w.Header().Add("Warning", `299 - "folder.grafana.app/v1beta1 is deprecated"`)
		// Only code 299 carries API warnings.
		w.Header().Add("Warning", `199 - "miscellaneous warning"`)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"apiVersion":"folder.grafana.app/v1","kind":"Folder","metadata":{"name":"f1"}}`))
	}))
	defer server.Close()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent.SetFlag(tt.agentMode)
			t.Cleanup(agent.ResetForTesting)

			var stderr bytes.Buffer
			ctx := config.ContextWithWarningWriter(t.Context(), &stderr)
			restCfg, err := config.NewNamespacedRESTConfig(ctx, config.Context{
				Grafana: &config.GrafanaConfig{Server: server.URL, StackID: 12345},
			})
			if err != nil {
				t.Fatalf("NewNamespacedRESTConfig: %v", err)
			}
			client, err := dynamic.NewForConfig(&restCfg.Config)
			if err != nil {
				t.Fatalf("dynamic.NewForConfig: %v", err)
			}

			folders := client.Resource(schema.GroupVersionResource{Group: "folder.grafana.app", Version: "v1", Resource: "folders"}).
				Namespace(restCfg.Namespace)
			// The same warnings on a second request are not repeated.
			for range 2 {
				if _, err := folders.Get(t.Context(), "f1", metav1.GetOptions{}); err != nil {
					t.Fatalf("get: %v", err)
				}
			}

			if got := stderr.String(); got != tt.want {
				t.Fatalf("warnings:\n got: %q\nwant: %q", got, tt.want)
			}
		})
	}
}

func TestNewNamespacedRESTConfig_NoWarningWriterKeepsClientDefault(t *testing.T) {
	restCfg, err := config.NewNamespacedRESTConfig(t.Context(), config.Context{
		Grafana: &config.GrafanaConfig{Server: "http://localhost:3000", StackID: 12345},
	})
	if err != nil {
		t.Fatalf("NewNamespacedRESTConfig: %v", err)
	}
	if restCfg.WarningHandlerWithContext != nil || restCfg.WarningHandler != nil {
		t.Fatal("expected client-go's default warning handling without a warning writer")
	}
}
