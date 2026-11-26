package discovery

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/rest"
)

// discoveryServer serves the given paths, counting requests. Unknown paths
// return 404, as Grafana does for /api.
func discoveryServer(t *testing.T, contentType string, paths map[string]string) (*rest.Config, *atomic.Int32) {
	t.Helper()
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		body, ok := paths[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return &rest.Config{Host: srv.URL}, &requests
}

func TestCachedClientAggregated(t *testing.T) {
	cfg, requests := discoveryServer(t, "application/json;g=apidiscovery.k8s.io;v=v2;as=APIGroupDiscoveryList", map[string]string{
		"/apis": `{"kind":"APIGroupDiscoveryList","items":[{"metadata":{"name":"a.grafana.app"},"versions":[
			{"version":"v2","freshness":"Stale"},
			{"version":"v1","resources":[{"resource":"things","responseKind":{"group":"a.grafana.app","version":"v1","kind":"Thing"},"scope":"Namespaced","verbs":["get"],
				"subresources":[{"subresource":"status","responseKind":{"group":"a.grafana.app","version":"v1","kind":"Thing"},"verbs":["get"]}]}]}]}]}`,
	})
	dir := t.TempDir()
	client, err := newCachedClient(cfg, dir)
	require.NoError(t, err)

	groups, lists, err := client.ServerGroupsAndResources()
	require.NoError(t, err)
	require.Len(t, groups, 1)
	assert.Equal(t, "a.grafana.app/v1", groups[0].PreferredVersion.GroupVersion, "the stale version is dropped")
	require.Len(t, lists, 1)
	names := make([]string, 0, len(lists[0].APIResources))
	for _, r := range lists[0].APIResources {
		names = append(names, r.Name)
		assert.True(t, r.Namespaced)
		assert.Equal(t, "Thing", r.Kind)
	}
	assert.Equal(t, []string{"things", "things/status"}, names)
	assert.Equal(t, int32(2), requests.Load(), "/api and /apis")

	cached, err := newCachedClient(cfg, dir)
	require.NoError(t, err)
	cachedGroups, cachedLists, err := cached.ServerGroupsAndResources()
	require.NoError(t, err)
	assert.Equal(t, int32(2), requests.Load(), "a second process reads the disk cache")
	assert.Equal(t, groups[0].PreferredVersion, cachedGroups[0].PreferredVersion)
	assert.Equal(t, lists[0].APIResources, cachedLists[0].APIResources)
}

func TestCachedClientLegacy(t *testing.T) {
	cfg, requests := discoveryServer(t, "application/json", map[string]string{
		"/apis":                  `{"kind":"APIGroupList","groups":[{"name":"a.grafana.app","versions":[{"groupVersion":"a.grafana.app/v1","version":"v1"}],"preferredVersion":{"groupVersion":"a.grafana.app/v1","version":"v1"}}]}`,
		"/apis/a.grafana.app/v1": `{"kind":"APIResourceList","groupVersion":"a.grafana.app/v1","resources":[{"name":"things","namespaced":true,"kind":"Thing","verbs":["get"]}]}`,
	})
	client, err := newCachedClient(cfg, t.TempDir())
	require.NoError(t, err)

	groups, lists, err := client.ServerGroupsAndResources()
	require.NoError(t, err)
	require.Len(t, groups, 1)
	require.Len(t, lists, 1)
	assert.Equal(t, "things", lists[0].APIResources[0].Name)
	assert.Equal(t, int32(3), requests.Load(), "/api, /apis and the group version")
}

func TestCachedClientStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"Unauthorized","code":401}`))
	}))
	t.Cleanup(srv.Close)
	client, err := newCachedClient(&rest.Config{Host: srv.URL}, t.TempDir())
	require.NoError(t, err)

	_, _, err = client.ServerGroupsAndResources()
	assert.True(t, apierrors.IsUnauthorized(err), "got %v", err)
}
