package collections_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/aio11y/aio11yhttp"
	"github.com/grafana/gcx/internal/providers/aio11y/eval/collections"
	"github.com/grafana/gcx/internal/providers/aio11y/eval/savedconversations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func writeJSON(w http.ResponseWriter, v any) {
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func newTestClient(t *testing.T, handler http.Handler) *collections.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: srv.URL},
		Namespace: "default",
	}
	base, err := aio11yhttp.NewClient(cfg)
	require.NoError(t, err)
	return collections.NewClient(base)
}

func TestClient_List(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/eval/collections")

		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, map[string]any{
			"items": []collections.Collection{
				{CollectionID: "c-1", Name: "Regression"},
				{CollectionID: "c-2", Name: "Smoke"},
			},
		})
	}))

	items, err := client.List(context.Background(), 0)
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, "c-1", items[0].CollectionID)
}

func TestClient_Get(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/eval/collections/c-1")

		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, collections.Collection{
			CollectionID: "c-1",
			Name:         "Regression",
			Description:  "Nightly run",
			MemberCount:  2,
			CreatedAt:    time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC),
		})
	}))

	col, err := client.Get(context.Background(), "c-1")
	require.NoError(t, err)
	assert.Equal(t, "c-1", col.CollectionID)
	assert.Equal(t, "Nightly run", col.Description)
	assert.Equal(t, 2, col.MemberCount)
}

func TestClient_Get_NotFound(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))

	_, err := client.Get(context.Background(), "missing")
	require.Error(t, err)
	require.ErrorIs(t, err, collections.ErrNotFound)
}

func TestClient_Create(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/plugins/grafana-sigil-app/resources/eval/collections", r.URL.Path)

		body, _ := io.ReadAll(r.Body)
		var raw map[string]any
		assert.NoError(t, json.Unmarshal(body, &raw))
		assert.Equal(t, "Regression", raw["name"])

		w.WriteHeader(http.StatusOK)
		writeJSON(w, collections.Collection{
			CollectionID: "c-99",
			Name:         "Regression",
			Description:  "Nightly",
		})
	}))

	col, err := client.Create(context.Background(), &collections.Collection{
		Name:        "Regression",
		Description: "Nightly",
	})
	require.NoError(t, err)
	assert.Equal(t, "c-99", col.CollectionID)
}

func TestClient_Update_PATCH(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPatch, r.Method)
		assert.Contains(t, r.URL.Path, "/eval/collections/c-1")

		body, _ := io.ReadAll(r.Body)
		var raw map[string]any
		assert.NoError(t, json.Unmarshal(body, &raw))
		assert.Equal(t, "renamed", raw["name"])
		_, hasDescription := raw["description"]
		assert.False(t, hasDescription, "description must be omitted when not set")
		_, hasUpdatedBy := raw["updated_by"]
		assert.False(t, hasUpdatedBy, "updated_by must not be sent by gcx")

		w.WriteHeader(http.StatusOK)
		writeJSON(w, collections.Collection{CollectionID: "c-1", Name: "renamed"})
	}))

	name := "renamed"
	col, err := client.Update(context.Background(), "c-1", &collections.UpdateRequest{Name: &name})
	require.NoError(t, err)
	assert.Equal(t, "renamed", col.Name)
}

func TestClient_Delete(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Contains(t, r.URL.Path, "/eval/collections/c-1")
		w.WriteHeader(http.StatusNoContent)
	}))

	require.NoError(t, client.Delete(context.Background(), "c-1"))
}

func TestClient_ListMembers(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/eval/collections/c-1/members")

		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, map[string]any{
			"items": []savedconversations.SavedConversation{
				{SavedID: "saved-1", Name: "first", ConversationID: "conv-1", Source: "telemetry"},
				{SavedID: "saved-2", Name: "second", ConversationID: "conv-2", Source: "manual"},
			},
		})
	}))

	items, err := client.ListMembers(context.Background(), "c-1", 0)
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, "saved-1", items[0].SavedID)
}

func TestClient_AddMembers(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.URL.Path, "/eval/collections/c-1/members")

		body, _ := io.ReadAll(r.Body)
		var raw map[string]any
		assert.NoError(t, json.Unmarshal(body, &raw))

		ids, ok := raw["saved_ids"].([]any)
		assert.True(t, ok)
		if ok {
			assert.Equal(t, "saved-1", ids[0])
			assert.Equal(t, "saved-2", ids[1])
		}

		_, hasAddedBy := raw["added_by"]
		assert.False(t, hasAddedBy, "added_by must not be sent by gcx")

		w.WriteHeader(http.StatusOK)
		writeJSON(w, map[string]string{"status": "ok"})
	}))

	require.NoError(t, client.AddMembers(context.Background(), "c-1", []string{"saved-1", "saved-2"}))
}

func TestClient_RemoveMember(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Contains(t, r.URL.Path, "/eval/collections/c-1/members/saved-1")
		w.WriteHeader(http.StatusNoContent)
	}))

	require.NoError(t, client.RemoveMember(context.Background(), "c-1", "saved-1"))
}

func TestStaticDescriptor(t *testing.T) {
	d := collections.StaticDescriptor()
	assert.Equal(t, "sigil.ext.grafana.app", d.GroupVersion.Group)
	assert.Equal(t, "v1alpha1", d.GroupVersion.Version)
	assert.Equal(t, "Collection", d.Kind)
	assert.Equal(t, "collections", d.Plural)
}

func TestCollectionSchema_NotEmpty(t *testing.T) {
	schema := collections.CollectionSchema()
	require.NotEmpty(t, schema)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(schema, &raw))
}

func TestSpecToUnstructured_StripsServerFields(t *testing.T) {
	col := collections.Collection{
		CollectionID: "c-1",
		TenantID:     "tenant-1",
		Name:         "Regression",
		Description:  "Nightly run",
		CreatedBy:    "alice",
		UpdatedBy:    "bob",
		MemberCount:  5,
		CreatedAt:    time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC),
		UpdatedAt:    time.Date(2026, 4, 2, 10, 0, 0, 0, time.UTC),
	}

	u, err := collections.SpecToUnstructured(col, "default")
	require.NoError(t, err)

	meta, ok := u.Object["metadata"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "c-1", meta["name"])
	assert.Equal(t, "default", meta["namespace"])

	spec, ok := u.Object["spec"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "Regression", spec["name"])
	assert.Equal(t, "Nightly run", spec["description"])

	for _, f := range []string{"collection_id", "tenant_id", "created_by", "updated_by", "created_at", "updated_at", "member_count"} {
		_, has := spec[f]
		assert.False(t, has, "spec must not contain server-managed field %q", f)
	}
}
