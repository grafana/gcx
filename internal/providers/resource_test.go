package providers_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

type resourceLoader struct {
	cfg   config.NamespacedRESTConfig
	err   error
	calls int
}

func (l *resourceLoader) LoadGrafanaConfig(context.Context) (config.NamespacedRESTConfig, error) {
	l.calls++
	return l.cfg, l.err
}

type resourceItem struct {
	Name string `json:"name"`
}

func (i resourceItem) GetResourceName() string { return i.Name }

func TestLoadGrafanaResource(t *testing.T) {
	failure := errors.New("failed")
	for _, tc := range []struct {
		name               string
		loadErr, clientErr error
	}{
		{name: "authenticated snapshot"},
		{name: "config failure", loadErr: failure},
		{name: "client failure", clientErr: failure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hits := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits++
				assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
				w.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()
			loader := &resourceLoader{cfg: config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL, BearerToken: "test-token"}, Namespace: "selected-stack"}, err: tc.loadErr}
			constructed := false
			resource := adapter.Resource[resourceItem]{Group: "test.grafana.app", Version: "v1", Kind: "Item",
				NewClient: func(ctx context.Context, deps adapter.ClientDeps) (any, error) {
					constructed = true
					if tc.clientErr != nil {
						return nil, tc.clientErr
					}
					req, err := http.NewRequestWithContext(ctx, http.MethodGet, deps.BaseURL, nil)
					require.NoError(t, err)
					resp, err := deps.HTTP.Do(req)
					require.NoError(t, err)
					require.NoError(t, resp.Body.Close())
					return struct{}{}, nil
				},
			}
			crud, cfg, err := providers.LoadGrafanaResource(t.Context(), loader, resource)
			assert.Equal(t, 1, loader.calls)
			if tc.loadErr != nil || tc.clientErr != nil {
				require.ErrorIs(t, err, failure)
				assert.Nil(t, crud)
				assert.Equal(t, tc.loadErr == nil, constructed)
				assert.Zero(t, hits)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, 1, hits)
			assert.Equal(t, "selected-stack", cfg.Namespace)
			obj, err := crud.ToUnstructured(resourceItem{Name: "one"})
			require.NoError(t, err)
			assert.Equal(t, "selected-stack", obj.GetNamespace())
		})
	}
}
