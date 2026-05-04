package synth_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_ListProbes(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/datasources/proxy/uid/sm-uid/sm/probe/list", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[
			{"id":1,"name":"Seoul","region":"APAC","online":true,"public":true,"tenantId":1,"latitude":37.5665,"longitude":126.978}
		]`))
	}))

	probes, err := client.ListProbes(context.Background(), "sm-uid")
	require.NoError(t, err)
	require.Len(t, probes, 1)
	assert.Equal(t, "Seoul", probes[0].Name)
	assert.Equal(t, "APAC", probes[0].Region)
	assert.True(t, probes[0].Online)
}

func TestClient_ListProbes_PropagatesError(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"plugin proxy route access denied"}`))
	}))

	_, err := client.ListProbes(context.Background(), "sm-uid")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "plugin proxy route access denied")
}
