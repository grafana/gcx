package ml

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/resources/remote"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type testRegistry struct{ desc resources.Descriptor }

func (r testRegistry) SupportedResources() resources.Descriptors {
	return resources.Descriptors{r.desc}
}

func TestPushMatchesTargetIDsAndRejectsAmbiguousNames(t *testing.T) {
	for _, decl := range []adapter.Declaration{jobs(), holidays(), outliers()} {
		for _, mode := range []string{"create", "update", "ambiguous"} {
			// Derive the resource descriptor before the server starts. Its dependency loader stays lazy.
			var baseURL string
			p := adapter.NewProvider("test", "test", func(context.Context) (adapter.ClientDeps, error) {
				return adapter.ClientDeps{BaseURL: baseURL, HTTP: http.DefaultClient, GrafanaURL: "https://target.example", Namespace: "default"}, nil
			}, decl)
			reg := p.TypedRegistrations()[0]
			t.Run(reg.GVK.Kind+"/"+mode, func(t *testing.T) {
				creates, updates := 0, 0
				items := []map[string]any{}
				if mode != "create" {
					items = append(items, map[string]any{"id": "target-id", "name": "forecast"})
				}
				if mode == "ambiguous" {
					items = append(items, map[string]any{"id": "another-id", "name": "Forecast!"})
				}
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					switch r.Method {
					case http.MethodGet:
						if strings.HasSuffix(r.URL.Path, "/source-id") {
							w.WriteHeader(http.StatusNotFound)
							fmt.Fprint(w, `{"message":"missing"}`)
							return
						}
						assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": items}))
					case http.MethodPost:
						var payload map[string]any
						assert.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
						assert.NotContains(t, payload, "id")
						if strings.HasSuffix(r.URL.Path, "/target-id") {
							updates++
						} else {
							creates++
						}
						payload["id"] = "target-id"
						items = []map[string]any{payload}
						assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": payload}))
					default:
						t.Errorf("unexpected request: %s", r.Method)
					}
				}))
				defer srv.Close()
				baseURL = srv.URL
				router := adapter.NewResourceClientRouter(nil, map[schema.GroupVersionKind]adapter.Factory{reg.GVK: reg.Factory})
				input := resources.MustFromObject(map[string]any{"apiVersion": reg.GVK.GroupVersion().String(), "kind": reg.GVK.Kind, "metadata": map[string]any{"name": "source-id", "namespace": "default"}, "spec": map[string]any{"name": "forecast", "metric": "requests"}}, resources.SourceInfo{})
				for range 2 {
					summary, err := remote.NewPusher(router, testRegistry{reg.Descriptor}).Push(t.Context(), remote.PushRequest{Resources: resources.NewResources(input), MaxConcurrency: 1, IncludeManaged: true, StopOnError: true, NoPushFailureLog: true})
					if mode == "ambiguous" {
						require.ErrorContains(t, err, "multiple "+reg.GVK.Kind+" resources")
						require.Equal(t, 1, summary.FailedCount())
					} else {
						require.NoError(t, err)
						require.Equal(t, 1, summary.SuccessCount())
					}
				}
				switch mode {
				case "create":
					require.Equal(t, 1, creates)
					require.Equal(t, 1, updates)
				case "update":
					require.Zero(t, creates)
					require.Equal(t, 2, updates)
				case "ambiguous":
					require.Zero(t, creates)
					require.Zero(t, updates)
				}
			})
		}
	}
}
