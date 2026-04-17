package org_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/org"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func newTestClient(t *testing.T, server *httptest.Server) *org.Client {
	t.Helper()
	cfg := config.NamespacedRESTConfig{
		Config: rest.Config{Host: server.URL},
	}
	client, err := org.NewClient(cfg)
	require.NoError(t, err)
	return client
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	_, _ = w.Write(data)
}

func TestClient_ListUsers(t *testing.T) {
	tests := []struct {
		name      string
		handler   http.HandlerFunc
		wantCount int
		wantErr   bool
	}{
		{
			name: "success with users",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/api/org/users", r.URL.Path)
				writeJSON(w, []org.OrgUser{
					{UserID: 1, Login: "alice", Email: "alice@example.com", Role: "Admin"},
					{UserID: 2, Login: "bob", Email: "bob@example.com", Role: "Editor"},
				})
			},
			wantCount: 2,
		},
		{
			name: "empty",
			handler: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, []org.OrgUser{})
			},
			wantCount: 0,
		},
		{
			name: "server error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"message":"boom"}`))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			users, err := client.ListUsers(t.Context())

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, users, tt.wantCount)
		})
	}
}

func TestClient_AddUser(t *testing.T) {
	tests := []struct {
		name    string
		req     org.AddUserRequest
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "success",
			req:  org.AddUserRequest{LoginOrEmail: "alice@example.com", Role: "Editor"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/api/org/users", r.URL.Path)

				body, err := io.ReadAll(r.Body)
				if !assert.NoError(t, err) {
					return
				}
				var got org.AddUserRequest
				if !assert.NoError(t, json.Unmarshal(body, &got)) {
					return
				}
				assert.Equal(t, "alice@example.com", got.LoginOrEmail)
				assert.Equal(t, "Editor", got.Role)

				writeJSON(w, map[string]any{"message": "user added to org"})
			},
		},
		{
			name: "missing role rejected by server",
			req:  org.AddUserRequest{LoginOrEmail: "alice@example.com"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"message":"role is required"}`))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			err := client.AddUser(t.Context(), tt.req)

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestClient_UpdateUserRole(t *testing.T) {
	tests := []struct {
		name    string
		userID  int
		role    string
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name:   "success",
			userID: 42,
			role:   "Admin",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPatch, r.Method)
				assert.Equal(t, "/api/org/users/42", r.URL.Path)

				body, err := io.ReadAll(r.Body)
				if !assert.NoError(t, err) {
					return
				}
				var got map[string]string
				if !assert.NoError(t, json.Unmarshal(body, &got)) {
					return
				}
				assert.Equal(t, "Admin", got["role"])

				writeJSON(w, map[string]any{"message": "role updated"})
			},
		},
		{
			name:    "not found",
			userID:  999,
			role:    "Viewer",
			handler: func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) },
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			err := client.UpdateUserRole(t.Context(), tt.userID, tt.role)

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestClient_RemoveUser(t *testing.T) {
	tests := []struct {
		name    string
		userID  int
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name:   "success",
			userID: 42,
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodDelete, r.Method)
				assert.Equal(t, "/api/org/users/42", r.URL.Path)
				writeJSON(w, map[string]any{"message": "user removed"})
			},
		},
		{
			name:   "not found",
			userID: 999,
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"message":"not found"}`))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			err := client.RemoveUser(t.Context(), tt.userID)

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}
