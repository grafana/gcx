package k6_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/grafana/gcx/internal/providers/k6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDirectClient_ListProjects_SendsBearerAndStackID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v3/account/grafana-app/start" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{
				"organization_id": "42", "v3_grafana_token": "v3-tok",
			})
			return
		}
		assert.Equal(t, "/cloud/v6/projects", r.URL.Path)
		assert.Equal(t, "Bearer v3-tok", r.Header.Get("Authorization"))
		assert.Equal(t, "999", r.Header.Get("X-Stack-Id"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"value": []map[string]any{{"id": 1, "name": "p1"}},
		})
	}))
	t.Cleanup(srv.Close)

	client := k6.NewDirectClient(context.Background(), srv.URL, nil)
	require.NoError(t, client.Authenticate(t.Context(), "glsa_test", 999))
	projects, err := client.ListProjects(t.Context())
	require.NoError(t, err)
	require.Len(t, projects, 1)
	assert.Equal(t, "p1", projects[0].Name)
}

func TestDirectClient_AuthenticateAndToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && r.URL.Path == "/v3/account/grafana-app/start" {
			assert.Equal(t, "glsa_test", r.Header.Get("X-Grafana-Service-Token"))
			assert.Equal(t, "999", r.Header.Get("X-Stack-Id"))
			assert.Equal(t, "admin", r.Header.Get("X-Grafana-User"))
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{
				"organization_id":  "42",
				"v3_grafana_token": "fresh-v3-token",
			})
			return
		}
		t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(srv.Close)

	client := k6.NewDirectClient(context.Background(), srv.URL, nil)
	err := client.Authenticate(t.Context(), "glsa_test", 999)
	require.NoError(t, err)

	tok, err := client.Token(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "fresh-v3-token", tok)
}

func TestDirectClient_EnvVars_RequireAuth(t *testing.T) {
	// Fresh client, never authenticated.
	client := k6.NewDirectClient(context.Background(), "http://unused", nil)

	_, err := client.ListEnvVars(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated")

	_, err = client.CreateEnvVar(t.Context(), "X", "y", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated")

	err = client.UpdateEnvVar(t.Context(), 1, "X", "y", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated")

	err = client.DeleteEnvVar(t.Context(), 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not authenticated")
}

func TestDirectClient_401WithReauthRetries(t *testing.T) {
	var apiCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v3/account/grafana-app/start" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{
				"organization_id": "42", "v3_grafana_token": "old",
			})
			return
		}
		call := apiCalls.Add(1)
		if call == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		assert.Equal(t, "Bearer new", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"value": []any{}})
	}))
	t.Cleanup(srv.Close)

	client := k6.NewDirectClient(context.Background(), srv.URL, nil)
	require.NoError(t, client.Authenticate(t.Context(), "glsa_test", 999))
	client.SetReauth(func(_ context.Context) (string, int, error) {
		return "new", 42, nil
	})
	_, err := client.ListProjects(t.Context())
	require.NoError(t, err)
	assert.Equal(t, int32(2), apiCalls.Load())
	tok, _ := client.Token(t.Context())
	assert.Equal(t, "new", tok)
}

func TestDirectClient_401WithReauthFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v3/account/grafana-app/start" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{
				"organization_id": "42", "v3_grafana_token": "tok",
			})
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	client := k6.NewDirectClient(context.Background(), srv.URL, nil)
	require.NoError(t, client.Authenticate(t.Context(), "glsa_test", 999))
	client.SetReauth(func(_ context.Context) (string, int, error) {
		return "", 0, errors.New("credential expired")
	})
	_, err := client.ListProjects(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "k6: reauth after 401:")
	assert.Contains(t, err.Error(), "credential expired")
}

func TestDirectClient_401WithoutReauthPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v3/account/grafana-app/start" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{
				"organization_id": "42", "v3_grafana_token": "tok",
			})
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	client := k6.NewDirectClient(context.Background(), srv.URL, nil)
	require.NoError(t, client.Authenticate(t.Context(), "glsa_test", 999))
	_, err := client.ListProjects(t.Context())
	require.Error(t, err)
}

func TestDirectClient_SetCachedAuth_SkipsExchange(t *testing.T) {
	var capturedAuth, capturedStackID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v3/account/grafana-app/start" {
			t.Fatalf("unexpected exchange call when cache was provided")
		}
		capturedAuth = r.Header.Get("Authorization")
		capturedStackID = r.Header.Get("X-Stack-Id")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"value": []any{}})
	}))
	t.Cleanup(srv.Close)

	client := k6.NewDirectClient(context.Background(), srv.URL, nil)
	client.SetCachedAuth("cached-v3", 42, 999)
	tok, err := client.Token(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "cached-v3", tok)
	_, err = client.ListProjects(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "Bearer cached-v3", capturedAuth)
	assert.Equal(t, "999", capturedStackID)
}

func TestDirectClient_doRaw_401WithReauthRetries(t *testing.T) {
	var scriptCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v3/account/grafana-app/start" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{
				"organization_id": "42", "v3_grafana_token": "old",
			})
			return
		}
		if r.Method == http.MethodPut && r.URL.Path == "/cloud/v6/load_tests/5/script" {
			call := scriptCalls.Add(1)
			if call == 1 {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			assert.Equal(t, "Bearer new", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusNoContent)
			return
		}
		t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(srv.Close)

	client := k6.NewDirectClient(context.Background(), srv.URL, nil)
	require.NoError(t, client.Authenticate(t.Context(), "glsa_test", 999))
	client.SetReauth(func(_ context.Context) (string, int, error) {
		return "new", 42, nil
	})
	err := client.UpdateLoadTestScript(t.Context(), 5, "export default function() {}")
	require.NoError(t, err)
	assert.Equal(t, int32(2), scriptCalls.Load())
}
