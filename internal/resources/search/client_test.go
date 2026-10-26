package search_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/resources/search"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

type failingTransport struct{ cause error }

func (t failingTransport) RoundTrip(*http.Request) (*http.Response, error) { return nil, t.cause }

func TestClientSearchSendsGlobalHybridRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/prefix/apis/search.grafana.app/v0alpha1/namespaces/org-1/global/search/hybrid", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		var body map[string]any
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "search.grafana.app/v0alpha1", body["apiVersion"])
		assert.Equal(t, "HybridSearchQuery", body["kind"])
		assert.Equal(t, "memory", body["query"])
		assert.InEpsilon(t, 50, body["limit"], 0.0001)
		assert.NotContains(t, body, "resources")
		_, _ = w.Write([]byte(`{"apiVersion":"search.grafana.app/v0alpha1","kind":"HybridSearchResults","items":[]}`))
	}))
	defer server.Close()
	client, err := search.NewClient(config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL + "/prefix"}, Namespace: "org-1"})
	require.NoError(t, err)
	result, err := client.Search(context.Background(), search.Query{Query: "memory", Limit: 50})
	require.NoError(t, err)
	require.NotNil(t, result.Items)
}

func TestClientSearchEscapesNamespaceAndPreservesOrderedFilters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/prefix/apis/search.grafana.app/v0alpha1/namespaces/org%2Fone/global/search/hybrid", r.URL.EscapedPath())
		var body struct {
			Resources []search.Resource `json:"resources"`
			Filters   []search.Filter   `json:"filters"`
		}
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, []search.Resource{{Resource: "dashboards", Group: "dashboard.grafana.app"}, {Resource: "folders", Group: "folder.grafana.app"}}, body.Resources)
		assert.Equal(t, []search.Filter{{Field: "folder", Values: []string{"", "team"}}}, body.Filters)
		_, _ = w.Write([]byte(`{"apiVersion":"search.grafana.app/v0alpha1","kind":"HybridSearchResults","items":[]}`))
	}))
	defer server.Close()
	client, err := search.NewClient(config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL + "/prefix"}, Namespace: "org/one"})
	require.NoError(t, err)
	_, err = client.Search(t.Context(), search.Query{Query: "q", Limit: 1, Resources: []search.Resource{{Resource: "dashboards", Group: "dashboard.grafana.app"}, {Resource: "folders", Group: "folder.grafana.app"}}, Filters: []search.Filter{{Field: "folder", Values: []string{"", "team"}}}})
	require.NoError(t, err)
}

func TestClientSearchPreservesHTTPStatusAndBoundsErrorBody(t *testing.T) {
	for _, code := range []int{400, 401, 403, 404, 422, 429, 501, 503} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(code)
				_, _ = w.Write([]byte(`{"message":"unavailable","traceID":"trace-1"}` + string(make([]byte, 10000))))
			}))
			defer server.Close()
			client, err := search.NewClient(config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}, Namespace: "org-1"})
			require.NoError(t, err)
			_, err = client.Search(t.Context(), search.Query{Query: "q", Limit: 50})
			var status *gcxerrors.HTTPStatusError
			require.ErrorAs(t, err, &status)
			require.Equal(t, code, status.Status)
			require.LessOrEqual(t, len(err.Error()), 4200)
		})
	}
}

func TestClientSearchRejectsMalformedResponseAndHonorsCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"kind":"Wrong","items":[]}`))
	}))
	defer server.Close()
	client, err := search.NewClient(config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}, Namespace: "org-1"})
	require.NoError(t, err)
	_, err = client.Search(t.Context(), search.Query{Query: "q", Limit: 50})
	require.ErrorContains(t, err, "invalid hybrid search response")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = client.Search(ctx, search.Query{Query: "q", Limit: 50})
	require.ErrorIs(t, err, context.Canceled)
}

func TestClientSearchRejectsTrailingResponseData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"apiVersion":"search.grafana.app/v0alpha1","kind":"HybridSearchResults","items":[]} {"unexpected":true}`))
	}))
	defer server.Close()
	client, err := search.NewClient(config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}, Namespace: "org-1"})
	require.NoError(t, err)
	_, err = client.Search(t.Context(), search.Query{Query: "q", Limit: 50})
	require.ErrorContains(t, err, "trailing")
}

func TestClientSearchReportsMissingDefaultsAndUncertainRouteWithoutFallback(t *testing.T) {
	for _, tc := range []struct {
		status  int
		message string
		want    string
	}{
		{status: 422, message: "resources are required", want: "supply --resource"},
		{status: 404, message: "route unavailable", want: "check deployment support"},
	} {
		t.Run(strconv.Itoa(tc.status), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				assert.Equal(t, http.MethodPost, r.Method)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"message":"` + tc.message + `","traceID":"trace-1"}`))
			}))
			defer server.Close()
			client, err := search.NewClient(config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}, Namespace: "org-1"})
			require.NoError(t, err)
			_, err = client.Search(t.Context(), search.Query{Query: "q", Limit: 50})
			var status *gcxerrors.HTTPStatusError
			require.ErrorAs(t, err, &status)
			require.Equal(t, tc.status, status.Status)
			require.Equal(t, "trace-1", status.TraceID)
			require.ErrorContains(t, err, tc.want)
			require.Equal(t, 1, calls)
		})
	}
}

func TestClientSearchPreservesPlainTextHTTPExplanation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("unsupported query syntax"))
	}))
	defer server.Close()
	client, err := search.NewClient(config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}, Namespace: "org-1"})
	require.NoError(t, err)
	_, err = client.Search(t.Context(), search.Query{Query: "q", Limit: 50})
	var status *gcxerrors.HTTPStatusError
	require.ErrorAs(t, err, &status)
	require.Equal(t, http.StatusBadRequest, status.Status)
	require.Equal(t, "unsupported query syntax", status.ServerMessage)
	require.Contains(t, status.Message, "unsupported query syntax")
	require.Equal(t, "text/plain", status.ContentType)
}

func TestClientSearchPreservesTransportFailureCause(t *testing.T) {
	cause := errors.New("fixture transport failed")
	client, err := search.NewClient(config.NamespacedRESTConfig{
		Config: rest.Config{
			Host:          "http://example.invalid",
			WrapTransport: func(http.RoundTripper) http.RoundTripper { return failingTransport{cause: cause} },
		},
		Namespace: "org-1",
	})
	require.NoError(t, err)
	_, err = client.Search(t.Context(), search.Query{Query: "q", Limit: 50})
	require.ErrorIs(t, err, cause)
	require.ErrorContains(t, err, "perform hybrid search")
}
