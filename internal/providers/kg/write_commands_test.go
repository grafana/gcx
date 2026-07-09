package kg_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	internalconfig "github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/kg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func TestBuildEntityWriteRequest(t *testing.T) {
	req, err := kg.BuildEntityWriteRequest("myapp", "Service", "checkout",
		map[string]string{"env": "prod"}, map[string]string{"team": "payments"}, "1h")
	require.NoError(t, err)
	assert.Equal(t, "myapp", req.Domain)
	assert.Equal(t, map[string]string{"env": "prod"}, req.Scope)
	require.NotNil(t, req.TTLSeconds)
	assert.Equal(t, int64(3600), *req.TTLSeconds)

	_, err = kg.BuildEntityWriteRequest("kg", "Service", "checkout", nil, nil, "")
	require.Error(t, err) // reserved domain

	// scope/property key validation (mirrors backend @SafeKgKeys)
	_, err = kg.BuildEntityWriteRequest("myapp", "Service", "x", map[string]string{"": "v"}, nil, "")
	require.Error(t, err, "empty scope key must be rejected")
	_, err = kg.BuildEntityWriteRequest("myapp", "Service", "x", map[string]string{"name": "v"}, nil, "")
	require.Error(t, err, "reserved scope key must be rejected")
	_, err = kg.BuildEntityWriteRequest("myapp", "Service", "x", map[string]string{"_x": "v"}, nil, "")
	require.Error(t, err, "underscore-prefixed scope key must be rejected")
	// scope/property overlap (mirrors backend @PropertiesNotInScope)
	_, err = kg.BuildEntityWriteRequest("myapp", "Service", "x", map[string]string{"env": "prod"}, map[string]string{"env": "staging"}, "")
	require.Error(t, err, "property shadowing a scope key must be rejected")
}

// writeLoaderFor returns a FakeWriteLoader pointed at server.
func writeLoaderFor(server *httptest.Server) *kg.FakeWriteLoader {
	return &kg.FakeWriteLoader{
		Cfg: internalconfig.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}, Namespace: "stack-123"},
	}
}

func TestEntitiesCreate_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, kg.EntityWriteResponse{Domain: "myapp", Type: "Service", Name: "checkout"})
	}))
	defer server.Close()
	cmd := kg.NewEntitiesCreateCommand(writeLoaderFor(server))
	cmd.SetArgs([]string{"--domain", "myapp", "--type", "Service", "--name", "checkout", "-o", "json"})
	require.NoError(t, cmd.Execute())
}

func TestEntitiesCreate_FileAndFlagsConflict(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	cmd := kg.NewEntitiesCreateCommand(writeLoaderFor(server))
	cmd.SetArgs([]string{"-f", "x.yaml", "--domain", "myapp", "--type", "Service", "--name", "checkout"})
	require.Error(t, cmd.Execute())
}

func TestEntitiesCreate_ClientSideValidation(t *testing.T) {
	cases := [][]string{
		{"--domain", "kg", "--type", "Service", "--name", "x"},
		{"--domain", "MyApp", "--type", "Service", "--name", "x"},
		{"--domain", "myapp", "--type", "9bad", "--name", "x"},
		{"--domain", "myapp", "--type", "Service", "--name", ""},
		{"--domain", "myapp", "--type", "Service", "--name", "x", "--scope", "noeq"},
		{"--domain", "myapp", "--type", "Service", "--name", "x", "--ttl", "nope"},
	}
	for _, args := range cases {
		called := false
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
		cmd := kg.NewEntitiesCreateCommand(writeLoaderFor(server))
		cmd.SetArgs(args)
		require.Error(t, cmd.Execute(), "args %v", args)
		assert.False(t, called, "validation must fail before HTTP for args %v", args)
		server.Close()
	}
}

func TestEntitiesDelete_ForceSendsDelete(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.True(t, strings.HasSuffix(r.URL.Path, "/entities"), "delete must hit collection path, got %s", r.URL.Path)
		assert.Equal(t, "myapp", r.URL.Query().Get("domain"))
		assert.Equal(t, "Service", r.URL.Query().Get("type"))
		assert.Equal(t, "checkout", r.URL.Query().Get("name"))
		assert.Equal(t, "prod", r.URL.Query().Get("scope[env]"), "scope must reach the server")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	cmd := kg.NewEntitiesDeleteCommand(writeLoaderFor(server))
	cmd.SetArgs([]string{"Service--checkout", "--domain", "myapp", "--scope", "env=prod", "--force"})
	require.NoError(t, cmd.Execute())
	assert.True(t, called)
}

func TestEntitiesDelete_RejectsBadType(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer server.Close()
	cmd := kg.NewEntitiesDeleteCommand(writeLoaderFor(server))
	cmd.SetArgs([]string{"9bad--checkout", "--domain", "myapp", "--force"})
	require.Error(t, cmd.Execute())
	assert.False(t, called, "invalid type must be rejected before HTTP")
}

func TestEntitiesCreate_RejectsBadScopeKeyNoHTTP(t *testing.T) {
	for _, scope := range []string{"=v", "name=x", "_x=v"} {
		called := false
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
		cmd := kg.NewEntitiesCreateCommand(writeLoaderFor(server))
		cmd.SetArgs([]string{"--domain", "myapp", "--type", "Service", "--name", "x", "--scope", scope})
		require.Error(t, cmd.Execute(), "scope %q must be rejected", scope)
		assert.False(t, called, "invalid scope key %q must not reach the wire", scope)
		server.Close()
	}
}

func TestEntitiesCreate_FileDefaultsNeverExpire(t *testing.T) {
	var gotTTL *int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body kg.EntityWriteRequest
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		gotTTL = body.TTLSeconds
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, kg.EntityWriteResponse{Domain: "myapp", Type: "Service", Name: "checkout"})
	}))
	defer server.Close()
	cmd := kg.NewEntitiesCreateCommand(writeLoaderFor(server))
	cmd.SetArgs([]string{"-f", "-", "-o", "json"})
	cmd.SetIn(bytes.NewBufferString("domain: myapp\ntype: Service\nname: checkout\n")) // no ttlSeconds
	require.NoError(t, cmd.Execute())
	require.NotNil(t, gotTTL, "ttlSeconds must be sent")
	assert.Equal(t, int64(-1), *gotTTL, "absent file ttlSeconds must default to never-expire (-1), not 0")
}

func TestEntitiesDelete_DeclineSkipsDelete(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer server.Close()
	cmd := kg.NewEntitiesDeleteCommand(writeLoaderFor(server))
	cmd.SetArgs([]string{"Service--checkout", "--domain", "myapp"})
	cmd.SetIn(bytes.NewBufferString("n\n"))
	cmd.SetOut(&bytes.Buffer{})
	require.NoError(t, cmd.Execute())
	assert.False(t, called, "declined confirmation must not issue DELETE")
}

func TestBuildRelationshipWriteRequest(t *testing.T) {
	req, err := kg.BuildRelationshipWriteRequest("myapp", "CALLS",
		"myapp/Service/checkout", map[string]string{"env": "prod"},
		"myapp/Service/cart", nil, nil, "1h")
	require.NoError(t, err)
	assert.Equal(t, "CALLS", req.Type)
	assert.Equal(t, "checkout", req.From.Name)
	assert.Equal(t, map[string]string{"env": "prod"}, req.From.Scope)
	assert.Equal(t, "cart", req.To.Name)
	require.NotNil(t, req.TTLSeconds)
	assert.Equal(t, int64(3600), *req.TTLSeconds)

	_, err = kg.BuildRelationshipWriteRequest("myapp", "CALLS", "bad-ref", nil, "myapp/Service/cart", nil, nil, "")
	require.Error(t, err) // malformed --from
	_, err = kg.BuildRelationshipWriteRequest("myapp", "CALLS", "myapp/Service/checkout", map[string]string{"_x": "v"}, "myapp/Service/cart", nil, nil, "")
	require.Error(t, err, "invalid from-scope key must be rejected")
}

func TestRelationshipsCreate_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		writeJSON(w, kg.RelationshipWriteResponse{Type: "CALLS"})
	}))
	defer server.Close()
	cmd := kg.NewRelationshipsCreateCommand(writeLoaderFor(server))
	cmd.SetArgs([]string{"--type", "CALLS", "--domain", "myapp",
		"--from", "myapp/Service/checkout", "--to", "myapp/Service/cart", "-o", "json"})
	require.NoError(t, cmd.Execute())
}

func TestRelationshipsCreate_BadRefValidation(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer server.Close()
	cmd := kg.NewRelationshipsCreateCommand(writeLoaderFor(server))
	cmd.SetArgs([]string{"--type", "CALLS", "--domain", "myapp", "--from", "bad", "--to", "myapp/Service/cart"})
	require.Error(t, cmd.Execute())
	assert.False(t, called)
}

func TestRelationshipsDelete_ForceSendsDelete(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.True(t, strings.HasSuffix(r.URL.Path, "/relationships"), "delete must hit collection path, got %s", r.URL.Path)
		assert.Equal(t, "CALLS", r.URL.Query().Get("type"))
		assert.Equal(t, "checkout", r.URL.Query().Get("from.name"))
		assert.Equal(t, "cart", r.URL.Query().Get("to.name"))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	cmd := kg.NewRelationshipsDeleteCommand(writeLoaderFor(server))
	cmd.SetArgs([]string{"--type", "CALLS", "--from", "myapp/Service/checkout", "--to", "myapp/Service/cart", "--force"})
	require.NoError(t, cmd.Execute())
	assert.True(t, called)
}

func TestRelationshipsDelete_DeclineSkips(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer server.Close()
	cmd := kg.NewRelationshipsDeleteCommand(writeLoaderFor(server))
	cmd.SetArgs([]string{"--type", "CALLS", "--from", "myapp/Service/checkout", "--to", "myapp/Service/cart"})
	cmd.SetIn(bytes.NewBufferString("n\n"))
	cmd.SetOut(&bytes.Buffer{})
	require.NoError(t, cmd.Execute())
	assert.False(t, called)
}
