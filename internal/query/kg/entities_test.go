package kg_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/query/kg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_LookupEntity(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		want    *kg.Entity
		wantErr bool
	}{
		{
			name: "200 decodes entity",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Contains(t, r.URL.Path, "v1/entity")
				q := r.URL.Query()
				assert.Equal(t, "service", q.Get("asserts_entity_type"))
				assert.Equal(t, "checkout", q.Get("asserts_entity_name"))
				assert.Equal(t, "prod", q.Get("env"))
				writeJSON(w, kg.Entity{Type: "service", Name: "checkout", Active: true})
			},
			want: &kg.Entity{Type: "service", Name: "checkout", Active: true},
		},
		{
			name: "204 not found",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			},
			want: nil,
		},
		{
			name: "500 returns api error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("boom"))
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()
			client := newTestClient(t, server)
			entity, err := client.LookupEntity(t.Context(), "service", "checkout", map[string]string{"env": "prod"}, "", 0, 0)
			if tt.wantErr {
				require.Error(t, err)
				var apiErr *kg.APIError
				require.ErrorAs(t, err, &apiErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, entity)
		})
	}
}

func TestClient_ListEntities(t *testing.T) {
	t.Run("request body shape", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Contains(t, r.URL.Path, "v1/search")

			var body struct {
				TimeCriteria struct {
					Start int64 `json:"start"`
					End   int64 `json:"end"`
				} `json:"timeCriteria"`
				ScopeCriteria struct {
					NameAndValues map[string][]string `json:"nameAndValues"`
				} `json:"scopeCriteria"`
				FilterCriteria []struct {
					EntityType       string `json:"entityType"`
					PropertyMatchers []struct {
						Name string `json:"name"`
						Op   string `json:"op"`
					} `json:"propertyMatchers"`
				} `json:"filterCriteria"`
				PageNum int `json:"pageNum"`
			}
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))

			assert.Equal(t, int64(1000), body.TimeCriteria.Start)
			assert.Equal(t, int64(2000), body.TimeCriteria.End)
			assert.Equal(t, []string{"prod"}, body.ScopeCriteria.NameAndValues["env"])
			if assert.Len(t, body.FilterCriteria, 1) {
				assert.Equal(t, "service", body.FilterCriteria[0].EntityType)
				if assert.Len(t, body.FilterCriteria[0].PropertyMatchers, 1) {
					assert.Equal(t, "name", body.FilterCriteria[0].PropertyMatchers[0].Name)
					assert.Equal(t, "IS NOT NULL", body.FilterCriteria[0].PropertyMatchers[0].Op)
				}
			}
			assert.Equal(t, 2, body.PageNum)

			writeJSON(w, map[string]any{
				"data": map[string]any{
					"entities": []map[string]any{
						{"type": "service", "name": "checkout", "active": true, "scope": map[string]string{"env": "prod"}},
					},
					"pageNum":                  2,
					"lastPage":                 true,
					"searchResultsMaxLimitHit": false,
				},
			})
		}))
		defer server.Close()
		client := newTestClient(t, server)

		page, err := client.ListEntities(t.Context(), "service", kg.EntityScope{Env: "prod"}, 1000, 2000, 2)
		require.NoError(t, err)
		require.Len(t, page.Entities, 1)
		assert.Equal(t, "service", page.Entities[0].Type)
		assert.Equal(t, "checkout", page.Entities[0].Name)
		assert.True(t, page.Entities[0].Active)
		assert.Equal(t, 2, page.PageNum)
		assert.True(t, page.LastPage)
		assert.False(t, page.MaxLimitHit)
	})

	t.Run("nil entities become empty slice", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"entities": nil,
					"pageNum":  0,
					"lastPage": true,
				},
			})
		}))
		defer server.Close()
		client := newTestClient(t, server)

		page, err := client.ListEntities(t.Context(), "service", kg.EntityScope{}, 0, 0, 0)
		require.NoError(t, err)
		assert.NotNil(t, page.Entities)
		assert.Empty(t, page.Entities)
	})

	t.Run("500 returns error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()
		client := newTestClient(t, server)

		_, err := client.ListEntities(t.Context(), "service", kg.EntityScope{}, 0, 0, 0)
		require.Error(t, err)
	})
}
