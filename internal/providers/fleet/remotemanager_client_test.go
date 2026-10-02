package fleet_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/providers/fleet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_ListClusters(t *testing.T) {
	t.Run("decodes clusters and treats a missing list as empty", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "/remotemanager.v1.RemoteManagerService/ListClusters", r.URL.Path)
			body := readBody(t, r)
			assert.JSONEq(t, `{}`, string(body))
			writeJSON(w, map[string]any{})
		}))
		t.Cleanup(server.Close)

		clusters, err := newTestClient(t, server).ListClusters(context.Background())
		require.NoError(t, err)
		assert.Empty(t, clusters)
		assert.NotNil(t, clusters)
	})

	t.Run("returns the clusters", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, map[string]any{
				"clusters": []map[string]any{
					{"id": "cluster-a", "name": "prod", "namespace": "alloy"},
				},
			})
		}))
		t.Cleanup(server.Close)

		clusters, err := newTestClient(t, server).ListClusters(context.Background())
		require.NoError(t, err)
		require.Len(t, clusters, 1)
		assert.Equal(t, fleet.Cluster{ID: "cluster-a", Name: "prod", Namespace: "alloy"}, clusters[0])
	})
}

func TestClient_UpdateClusterSendsEmptyFields(t *testing.T) {
	var path string
	var got map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		decodeBody(t, r, &got)
		writeJSON(w, got)
	}))
	t.Cleanup(server.Close)

	updated, err := newTestClient(t, server).UpdateCluster(context.Background(), fleet.Cluster{ID: "cluster-a"})
	require.NoError(t, err)
	assert.Equal(t, "/remotemanager.v1.RemoteManagerService/UpdateCluster", path)
	assert.Equal(t, map[string]string{"id": "cluster-a", "name": "", "namespace": ""}, got)
	assert.Equal(t, "cluster-a", updated.ID)
}

func TestClient_CreateClusterSendsEmptyNamespace(t *testing.T) {
	var path string
	var got map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		decodeBody(t, r, &got)
		writeJSON(w, got)
	}))
	t.Cleanup(server.Close)

	created, err := newTestClient(t, server).CreateCluster(context.Background(), fleet.Cluster{ID: "cluster-a", Name: "prod"})
	require.NoError(t, err)
	assert.Equal(t, "/remotemanager.v1.RemoteManagerService/CreateCluster", path)
	assert.Equal(t, map[string]string{"id": "cluster-a", "name": "prod", "namespace": ""}, got)
	assert.Empty(t, created.Namespace)
}

func TestClient_DeleteCluster(t *testing.T) {
	var path string
	var got map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		decodeBody(t, r, &got)
		writeJSON(w, map[string]any{})
	}))
	t.Cleanup(server.Close)

	err := newTestClient(t, server).DeleteCluster(context.Background(), "cluster-a")
	require.NoError(t, err)
	assert.Equal(t, "/remotemanager.v1.RemoteManagerService/DeleteCluster", path)
	assert.Equal(t, "cluster-a", got["id"])
}

func TestClient_ListCollectorCRs(t *testing.T) {
	t.Run("omits clusterId when listing every CR", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/remotemanager.v1.RemoteManagerService/ListCollectorCRs", r.URL.Path)
			body := readBody(t, r)
			assert.JSONEq(t, `{}`, string(body))
			assert.NotContains(t, string(body), "clusterId")
			writeJSON(w, map[string]any{})
		}))
		t.Cleanup(server.Close)

		crs, err := newTestClient(t, server).ListCollectorCRs(context.Background(), "")
		require.NoError(t, err)
		assert.NotNil(t, crs)
		assert.Empty(t, crs)
	})

	t.Run("sends clusterId when set", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]string
			decodeBody(t, r, &body)
			assert.Equal(t, "cluster-a", body["clusterId"])
			writeJSON(w, map[string]any{
				"collectorCrs": []map[string]any{{
					"id":              "cr-a",
					"clusterId":       "cluster-a",
					"namespace":       "alloy",
					"name":            "metrics",
					"release":         "k8smon",
					"spec":            "spec: {}\n",
					"revision":        "rev-1",
					"appliedRevision": "rev-1",
					"applyError":      "",
				}},
			})
		}))
		t.Cleanup(server.Close)

		crs, err := newTestClient(t, server).ListCollectorCRs(context.Background(), "cluster-a")
		require.NoError(t, err)
		require.Len(t, crs, 1)
		assert.Equal(t, "rev-1", crs[0].Revision)
		assert.Equal(t, "rev-1", crs[0].AppliedRevision)
		assert.Equal(t, "spec: {}\n", crs[0].Spec)
	})
}

func TestClient_CreateCollectorCROmitsServerFields(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/remotemanager.v1.RemoteManagerService/CreateCollectorCR", r.URL.Path)
		decodeBody(t, r, &got)
		writeJSON(w, map[string]any{
			"id": "cr-a", "clusterId": "cluster-a", "spec": "spec: {}\n", "revision": "rev-1",
		})
	}))
	t.Cleanup(server.Close)

	created, err := newTestClient(t, server).CreateCollectorCR(context.Background(), fleet.CollectorCR{
		ID:        "cr-a",
		ClusterID: "cluster-a",
		Spec:      "spec: {}\n",
		Revision:  "client-should-be-ignored",
	})
	require.NoError(t, err)
	assert.Equal(t, "rev-1", created.Revision)
	assert.Equal(t, "spec: {}\n", got["spec"])
	assert.NotContains(t, got, "revision")
	assert.NotContains(t, got, "appliedRevision")
	assert.NotContains(t, got, "applyError")
	assert.Empty(t, got["namespace"])
}

func TestClient_DeleteCollectorCR(t *testing.T) {
	var path string
	var got map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		decodeBody(t, r, &got)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	err := newTestClient(t, server).DeleteCollectorCR(context.Background(), "cr-a")
	require.NoError(t, err)
	assert.Equal(t, "/remotemanager.v1.RemoteManagerService/DeleteCollectorCR", path)
	assert.Equal(t, "cr-a", got["id"])
}

func TestClient_RemoteManagerHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"code":"already_exists","message":"cluster already exists"}`))
	}))
	t.Cleanup(server.Close)

	_, err := newTestClient(t, server).CreateCluster(context.Background(), fleet.Cluster{ID: "cluster-a"})
	require.ErrorContains(t, err, "already_exists")
	require.ErrorContains(t, err, "CreateCluster")
}

func readBody(t *testing.T, r *http.Request) []byte {
	t.Helper()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Errorf("read body: %v", err)
		return nil
	}
	return body
}

func decodeBody(t *testing.T, r *http.Request, dest any) {
	t.Helper()
	if err := json.NewDecoder(r.Body).Decode(dest); err != nil {
		t.Errorf("decode body: %v", err)
	}
}
