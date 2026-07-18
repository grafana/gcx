package annotations_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/annotations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func newClient(t *testing.T, url string) *annotations.Client {
	t.Helper()
	c, err := annotations.NewClient(config.NamespacedRESTConfig{Config: rest.Config{Host: url}})
	require.NoError(t, err)
	return c
}

func TestClient_List(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/annotations", r.URL.Path)
		q := r.URL.Query()
		assert.Equal(t, "1000", q.Get("from"))
		assert.Equal(t, "2000", q.Get("to"))
		assert.ElementsMatch(t, []string{"deploy", "prod"}, q["tags"])
		assert.Equal(t, "25", q.Get("limit"))
		_, _ = w.Write([]byte(`[{"id":1,"text":"a"},{"id":2,"text":"b"}]`))
	}))
	defer srv.Close()

	got, err := newClient(t, srv.URL).List(context.Background(), annotations.ListOptions{
		From: 1000, To: 2000, Tags: []string{"deploy", "prod"}, Limit: 25,
	})
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, int64(1), got[0].ID)
}

func TestClient_Get(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/annotations/7", r.URL.Path)
		_, _ = w.Write([]byte(`{"id":7,"text":"hello"}`))
	}))
	defer srv.Close()

	got, err := newClient(t, srv.URL).Get(context.Background(), 7)
	require.NoError(t, err)
	assert.Equal(t, int64(7), got.ID)
	assert.Equal(t, "hello", got.Text)
}

func TestClient_Create(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		_, _ = w.Write([]byte(`{"id":99,"message":"Annotation added"}`))
	}))
	defer srv.Close()

	a := &annotations.Annotation{Text: "deploy"}
	err := newClient(t, srv.URL).Create(context.Background(), a)
	require.NoError(t, err)
	assert.Equal(t, int64(99), a.ID)
	assert.Equal(t, "deploy", gotBody["text"])
}

func TestClient_Update(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPatch, r.Method)
		assert.Equal(t, "/api/annotations/5", r.URL.Path)
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		_, _ = w.Write([]byte(`{"message":"Annotation patched"}`))
	}))
	defer srv.Close()

	err := newClient(t, srv.URL).Update(context.Background(), 5, map[string]any{"text": "updated"})
	require.NoError(t, err)
	assert.Equal(t, "updated", gotBody["text"])
}

func TestClient_Delete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/api/annotations/3", r.URL.Path)
		_, _ = w.Write([]byte(`{"message":"Annotation deleted"}`))
	}))
	defer srv.Close()

	err := newClient(t, srv.URL).Delete(context.Background(), 3)
	require.NoError(t, err)
}

func TestClient_Tags(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/annotations/tags", r.URL.Path)
		_, _ = w.Write([]byte(`{"result":{"tags":[{"tag":"deploy","count":3},{"tag":"prod","count":7}]}}`))
	}))
	defer srv.Close()

	tags, err := newClient(t, srv.URL).Tags(context.Background())
	require.NoError(t, err)
	require.Len(t, tags, 2)
	assert.Equal(t, "deploy", tags[0].Tag)
	assert.Equal(t, int64(7), tags[1].Count)
}

func TestClient_MassDelete(t *testing.T) {
	var gotBody annotations.MassDeleteRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/annotations/mass-delete", r.URL.Path)
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		_, _ = w.Write([]byte(`{"message":"Annotations deleted"}`))
	}))
	defer srv.Close()

	err := newClient(t, srv.URL).MassDelete(context.Background(), annotations.MassDeleteRequest{
		DashboardUID: "abc", PanelID: 3,
	})
	require.NoError(t, err)
	assert.Equal(t, "abc", gotBody.DashboardUID)
	assert.Equal(t, int64(3), gotBody.PanelID)
}

func TestClient_ErrorParsesMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"forbidden"}`))
	}))
	defer srv.Close()

	_, err := newClient(t, srv.URL).Get(context.Background(), 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
	assert.Contains(t, err.Error(), "forbidden")
}
