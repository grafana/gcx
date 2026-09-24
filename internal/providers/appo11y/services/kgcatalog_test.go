package services //nolint:testpackage // Tests drive the unexported kgCatalog/kgFlags/annotateServicesFromKG surface directly.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func newKGTestConfig(host string) config.NamespacedRESTConfig {
	return config.NamespacedRESTConfig{
		Config:    rest.Config{Host: host},
		Namespace: "stack-123",
	}
}

func writeKGJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		panic(err)
	}
}

func TestResolveKGMode(t *testing.T) {
	tests := []struct {
		raw     string
		want    kgMode
		wantErr bool
	}{
		{raw: "auto", want: kgModeAuto},
		{raw: "off", want: kgModeOff},
		{raw: "AUTO", want: kgModeAuto},
		{raw: "Off", want: kgModeOff},
		{raw: "bogus", wantErr: true},
		{raw: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			got, err := resolveKGMode(tt.raw)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNewKGCatalog(t *testing.T) {
	t.Run("off returns nil without any HTTP calls", func(t *testing.T) {
		called := false
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		cat := newKGCatalog(newKGTestConfig(srv.URL), kgModeOff)
		assert.Nil(t, cat)
		assert.False(t, called)
	})

	t.Run("auto with a valid config returns non-nil", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()

		cat := newKGCatalog(newKGTestConfig(srv.URL), kgModeAuto)
		assert.NotNil(t, cat)
	})
}

func TestKGCatalogLookup(t *testing.T) {
	t.Run("active and found returns a populated ref", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.Contains(r.URL.Path, "v1/stack/status"):
				writeKGJSON(w, map[string]any{"enabled": true, "status": "complete"})
			case strings.Contains(r.URL.Path, "v1/entity"):
				writeKGJSON(w, map[string]any{"type": "Service", "name": "checkout", "scope": map[string]string{"env": "prod"}})
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer srv.Close()

		cat := newKGCatalog(newKGTestConfig(srv.URL), kgModeAuto)
		require.NotNil(t, cat)

		ref := cat.lookupVerbose(context.Background(), "checkout", 0, 0).ref
		require.NotNil(t, ref)
		assert.Equal(t, "Service", ref.EntityType)
		assert.Equal(t, map[string]string{"env": "prod"}, ref.Scope)
	})

	// TestKGCatalogLookup/scope-less_miss_falls_back_to_a_name-exact_search
	// guards the fix for LookupEntity's scope requirement: a service the
	// graph only knows under a specific scope (env/site/namespace) won't
	// match a scope-less LookupEntity call, so lookupVerbose must fall back
	// to a server-side name-exact search — the same two-step
	// internal/providers/kg's discoverEntityScope uses.
	t.Run("scope-less miss falls back to a name-exact search", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.Contains(r.URL.Path, "v1/stack/status"):
				writeKGJSON(w, map[string]any{"enabled": true, "status": "complete"})
			case strings.Contains(r.URL.Path, "v1/entity"):
				w.WriteHeader(http.StatusNoContent) // scope-less LookupEntity misses
			case strings.Contains(r.URL.Path, "v1/search"):
				var body struct {
					TimeCriteria struct {
						Start int64 `json:"start"`
						End   int64 `json:"end"`
					} `json:"timeCriteria"`
					FilterCriteria []struct {
						PropertyMatchers []struct {
							Name  string `json:"name"`
							Op    string `json:"op"`
							Value string `json:"value"`
						} `json:"propertyMatchers"`
					} `json:"filterCriteria"`
				}
				if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&body)) {
					return
				}
				assert.Equal(t, int64(1000), body.TimeCriteria.Start)
				assert.Equal(t, int64(2000), body.TimeCriteria.End)
				if !assert.Len(t, body.FilterCriteria, 1) {
					return
				}
				if !assert.Len(t, body.FilterCriteria[0].PropertyMatchers, 1) {
					return
				}
				matcher := body.FilterCriteria[0].PropertyMatchers[0]
				assert.Equal(t, "name", matcher.Name)
				assert.Equal(t, "=", matcher.Op)
				assert.Equal(t, "checkout", matcher.Value)
				writeKGJSON(w, map[string]any{
					"data": map[string]any{
						"entities": []map[string]any{
							{"type": "Service", "name": "checkout-worker", "scope": map[string]string{"env": "wrong"}},
							{"type": "Service", "name": "checkout", "scope": map[string]string{"env": "prod"}},
						},
						"lastPage": true,
					},
				})
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer srv.Close()

		cat := newKGCatalog(newKGTestConfig(srv.URL), kgModeAuto)
		require.NotNil(t, cat)

		ref := cat.lookupVerbose(context.Background(), "checkout", 1000, 2000).ref
		require.NotNil(t, ref)
		assert.Equal(t, "Service", ref.EntityType)
		assert.Equal(t, map[string]string{"env": "prod"}, ref.Scope)
	})

	t.Run("active but not found in either lookup or search returns nil", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.Contains(r.URL.Path, "v1/stack/status"):
				writeKGJSON(w, map[string]any{"enabled": true, "status": "complete"})
			case strings.Contains(r.URL.Path, "v1/entity"):
				w.WriteHeader(http.StatusNoContent)
			case strings.Contains(r.URL.Path, "v1/search"):
				writeKGJSON(w, map[string]any{"data": map[string]any{"entities": []map[string]any{}, "lastPage": true}})
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer srv.Close()

		cat := newKGCatalog(newKGTestConfig(srv.URL), kgModeAuto)
		require.NotNil(t, cat)
		assert.Nil(t, cat.lookupVerbose(context.Background(), "unknown", 0, 0).ref)
	})

	t.Run("inactive short-circuits before the entity endpoint is hit", func(t *testing.T) {
		var entityHit bool
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.Contains(r.URL.Path, "v1/stack/status"):
				w.WriteHeader(http.StatusNotFound) // Active() -> (false, nil)
			case strings.Contains(r.URL.Path, "v1/entity"):
				entityHit = true
				writeKGJSON(w, map[string]any{"type": "Service", "name": "checkout"})
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer srv.Close()

		cat := newKGCatalog(newKGTestConfig(srv.URL), kgModeAuto)
		require.NotNil(t, cat)
		assert.Nil(t, cat.lookupVerbose(context.Background(), "checkout", 0, 0).ref)
		assert.False(t, entityHit, "LookupEntity's endpoint must not be hit once KG is known inactive")
	})

	t.Run("active but the entity lookup itself errors is inconclusive, not swallowed", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.Contains(r.URL.Path, "v1/stack/status"):
				writeKGJSON(w, map[string]any{"enabled": true, "status": "complete"})
			case strings.Contains(r.URL.Path, "v1/entity"):
				w.WriteHeader(http.StatusInternalServerError)
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer srv.Close()

		cat := newKGCatalog(newKGTestConfig(srv.URL), kgModeAuto)
		require.NotNil(t, cat)
		lr := cat.lookupVerbose(context.Background(), "checkout", 0, 0)
		assert.Nil(t, lr.ref)
		assert.True(t, lr.inconclusive)
	})
}

func TestKGCatalogIndex(t *testing.T) {
	t.Run("active with entities returns a correctly-keyed map", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.Contains(r.URL.Path, "v1/stack/status"):
				writeKGJSON(w, map[string]any{"enabled": true, "status": "complete"})
			case strings.Contains(r.URL.Path, "v1/search"):
				writeKGJSON(w, map[string]any{
					"data": map[string]any{
						"entities": []map[string]any{
							{"type": "Service", "name": "checkout", "scope": map[string]string{"env": "prod"}},
							{"type": "Service", "name": "payments"},
						},
						"lastPage": true,
					},
				})
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer srv.Close()

		cat := newKGCatalog(newKGTestConfig(srv.URL), kgModeAuto)
		require.NotNil(t, cat)

		result := cat.index(context.Background(), 0, 0)
		require.False(t, result.inconclusive)
		require.False(t, result.truncated)
		idx := result.idx
		require.Len(t, idx, 2)
		require.NotNil(t, idx["checkout"])
		assert.Equal(t, map[string]string{"env": "prod"}, idx["checkout"].Scope)
		require.NotNil(t, idx["payments"])
	})

	t.Run("first page not last and non-empty is reported as truncated", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.Contains(r.URL.Path, "v1/stack/status"):
				writeKGJSON(w, map[string]any{"enabled": true, "status": "complete"})
			case strings.Contains(r.URL.Path, "v1/search"):
				writeKGJSON(w, map[string]any{
					"data": map[string]any{
						"entities": []map[string]any{{"type": "Service", "name": "checkout"}},
						"lastPage": false,
					},
				})
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer srv.Close()

		cat := newKGCatalog(newKGTestConfig(srv.URL), kgModeAuto)
		require.NotNil(t, cat)

		result := cat.index(context.Background(), 0, 0)
		assert.True(t, result.truncated)
		assert.False(t, result.inconclusive)
		assert.Len(t, result.idx, 1)
	})

	t.Run("inactive returns an empty map without hitting the search endpoint", func(t *testing.T) {
		var searchHit bool
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.Contains(r.URL.Path, "v1/stack/status"):
				w.WriteHeader(http.StatusNotFound)
			case strings.Contains(r.URL.Path, "v1/search"):
				searchHit = true
				writeKGJSON(w, map[string]any{"data": map[string]any{"entities": []map[string]any{}}})
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer srv.Close()

		cat := newKGCatalog(newKGTestConfig(srv.URL), kgModeAuto)
		require.NotNil(t, cat)

		result := cat.index(context.Background(), 0, 0)
		assert.False(t, result.inconclusive)
		assert.False(t, result.truncated)
		assert.NotNil(t, result.idx)
		assert.Empty(t, result.idx)
		assert.False(t, searchHit, "ListEntities's endpoint must not be hit once KG is known inactive")
	})

	t.Run("active but the search itself errors returns an empty map and is marked inconclusive", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.Contains(r.URL.Path, "v1/stack/status"):
				writeKGJSON(w, map[string]any{"enabled": true, "status": "complete"})
			case strings.Contains(r.URL.Path, "v1/search"):
				w.WriteHeader(http.StatusInternalServerError)
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer srv.Close()

		cat := newKGCatalog(newKGTestConfig(srv.URL), kgModeAuto)
		require.NotNil(t, cat)

		result := cat.index(context.Background(), 0, 0)
		assert.NotNil(t, result.idx)
		assert.Empty(t, result.idx)
		assert.True(t, result.inconclusive, "a search failure must be distinguishable from a genuine empty catalog")
		assert.Error(t, result.inconclusiveErr)
	})
}

// TestKGCatalogLookupVerbose_Inconclusive guards the fix for the "swallowed
// error" finding: an Active() or LookupEntity failure must be reported as
// inconclusive (so a caller can warn), not collapse into the same shape as
// "the graph genuinely doesn't know this service".
func TestKGCatalogLookupVerbose_Inconclusive(t *testing.T) {
	t.Run("Active() failure is inconclusive", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		cat := newKGCatalog(newKGTestConfig(srv.URL), kgModeAuto)
		require.NotNil(t, cat)

		lr := cat.lookupVerbose(context.Background(), "checkout", 0, 0)
		assert.Nil(t, lr.ref)
		assert.True(t, lr.inconclusive)
		assert.Error(t, lr.inconclusiveErr)
	})

	t.Run("LookupEntity failure is inconclusive", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.Contains(r.URL.Path, "v1/stack/status"):
				writeKGJSON(w, map[string]any{"enabled": true, "status": "complete"})
			case strings.Contains(r.URL.Path, "v1/entity"):
				w.WriteHeader(http.StatusInternalServerError)
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer srv.Close()

		cat := newKGCatalog(newKGTestConfig(srv.URL), kgModeAuto)
		require.NotNil(t, cat)

		lr := cat.lookupVerbose(context.Background(), "checkout", 0, 0)
		assert.Nil(t, lr.ref)
		assert.True(t, lr.inconclusive)
		assert.Error(t, lr.inconclusiveErr)
	})

	t.Run("name search failure is inconclusive", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.Contains(r.URL.Path, "v1/stack/status"):
				writeKGJSON(w, map[string]any{"enabled": true, "status": "complete"})
			case strings.Contains(r.URL.Path, "v1/entity"):
				w.WriteHeader(http.StatusNoContent)
			case strings.Contains(r.URL.Path, "v1/search"):
				w.WriteHeader(http.StatusInternalServerError)
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer srv.Close()

		cat := newKGCatalog(newKGTestConfig(srv.URL), kgModeAuto)
		require.NotNil(t, cat)

		lr := cat.lookupVerbose(context.Background(), "checkout", 0, 0)
		assert.Nil(t, lr.ref)
		assert.True(t, lr.inconclusive)
		assert.Error(t, lr.inconclusiveErr)
	})

	t.Run("inactive is a genuine negative, not inconclusive", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()

		cat := newKGCatalog(newKGTestConfig(srv.URL), kgModeAuto)
		require.NotNil(t, cat)

		lr := cat.lookupVerbose(context.Background(), "checkout", 0, 0)
		assert.Nil(t, lr.ref)
		assert.False(t, lr.inconclusive)
		assert.NoError(t, lr.inconclusiveErr)
	})
}

func TestAnnotateServicesFromKG(t *testing.T) {
	items := []Service{{Name: "checkout"}, {Name: "payments"}}
	idx := map[string]*KGRef{"checkout": {EntityType: "Service"}}

	got := annotateServicesFromKG(items, idx)
	require.Len(t, got, 2)
	assert.Equal(t, &KGRef{EntityType: "Service"}, got[0].KG)
	assert.Nil(t, got[1].KG, "an item with no matching KG index entry must stay nil, not a synthesized {Known:false}")

	// An index entry with no matching item must not add a new row — the
	// graph annotates existing telemetry-discovered rows, it never adds ones.
	idx["orphan-service"] = &KGRef{EntityType: "Service"}
	got2 := annotateServicesFromKG(items, idx)
	assert.Len(t, got2, 2, "a KG index entry with no matching item must not synthesize a new row")
}

// kgRegressionServer mocks a stack where the App O11y activation check
// succeeds and every Prometheus query returns an empty vector, but the
// Knowledge Graph status endpoint 404s (not installed). It's used to prove
// --kg's additive-only contract: a command's JSON output must carry no "kg"
// key at all when the graph is off or inactive, exactly as before this flag
// existed.
func kgRegressionServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == activationEndpoint:
			w.WriteHeader(http.StatusOK)
		case strings.Contains(r.URL.Path, "grafana-asserts-app/resources"):
			w.WriteHeader(http.StatusNotFound)
		case r.URL.Path == "/bootdata":
			http.Error(w, `{"message":"not a cloud stack"}`, http.StatusNotFound)
		default:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestServicesCommands_KGAdditiveOnly is the regression guarantee: for each
// of the four gated commands, neither --kg off nor --kg auto against a stack
// where the Knowledge Graph is inactive may introduce a "kg" key into the
// JSON output — the two modes must be indistinguishable from a pre-flag
// world on a stack that doesn't have the graph.
func TestServicesCommands_KGAdditiveOnly(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "list --kg off", args: []string{"list", "-d", "test-uid", "-o", "json", "--kg", "off"}},
		{name: "list --kg auto (default, kg inactive)", args: []string{"list", "-d", "test-uid", "-o", "json"}},
		{name: "get --kg off", args: []string{"get", "checkoutservice", "-d", "test-uid", "-o", "json", "--kg", "off"}},
		{name: "get --kg auto (default, kg inactive)", args: []string{"get", "checkoutservice", "-d", "test-uid", "-o", "json"}},
		{name: "map --kg off", args: []string{"map", "checkoutservice", "-d", "test-uid", "-o", "json", "--kg", "off"}},
		{name: "map --kg auto (default, kg inactive)", args: []string{"map", "checkoutservice", "-d", "test-uid", "-o", "json"}},
		{name: "list-operations --kg off", args: []string{"list-operations", "checkoutservice", "-d", "test-uid", "-o", "json", "--kg", "off"}},
		{name: "list-operations --kg auto (default, kg inactive)", args: []string{"list-operations", "checkoutservice", "-d", "test-uid", "-o", "json"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := kgRegressionServer(t)
			loader := newActivationTestLoader(t, srv.URL)

			root := Commands(loader)
			root.SilenceUsage = true
			root.SilenceErrors = true
			var stdout bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(io.Discard)
			root.SetIn(strings.NewReader(""))
			root.SetArgs(tc.args)
			_ = root.Execute() // may fail with the standard "no telemetry" not-found error; irrelevant here

			require.NotEmpty(t, stdout.String(), "command must still emit its result document")
			assert.NotContains(t, stdout.String(), `"kg"`, "no kg key may appear when the graph is off or inactive")

			var decoded any
			require.NoError(t, json.Unmarshal(stdout.Bytes(), &decoded), "stdout must still be valid JSON")
		})
	}
}
