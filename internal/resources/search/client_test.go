package search_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/resources/search"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

type failingTransport struct{ cause error }

func (t failingTransport) RoundTrip(*http.Request) (*http.Response, error) { return nil, t.cause }

type searchRoundTripFunc func(*http.Request) (*http.Response, error)

func (f searchRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type searchReadBody struct {
	reader io.Reader
	read   int
}

func (b *searchReadBody) Read(p []byte) (int, error) {
	n, err := b.reader.Read(p)
	b.read += n
	return n, err
}

func (b *searchReadBody) Close() error { return nil }

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
	require.EqualError(t, err, `invalid hybrid search response: expected kind HybridSearchResults, got "Wrong"`)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = client.Search(ctx, search.Query{Query: "q", Limit: 50})
	require.ErrorIs(t, err, context.Canceled)
}

func TestClientSearchAcceptsHybridResultsWithOptionalEnvelopeFields(t *testing.T) {
	for _, tc := range []struct {
		name           string
		body           string
		wantAPIVersion string
		wantItemsNil   bool
	}{
		{name: "null items", body: `{"apiVersion":"search.grafana.app/v0alpha1","kind":"HybridSearchResults","items":null}`, wantAPIVersion: "search.grafana.app/v0alpha1", wantItemsNil: true},
		{name: "omitted items", body: `{"apiVersion":"search.grafana.app/v0alpha1","kind":"HybridSearchResults"}`, wantAPIVersion: "search.grafana.app/v0alpha1", wantItemsNil: true},
		{name: "different api version", body: `{"apiVersion":"search.grafana.app/v1","kind":"HybridSearchResults","items":[]}`, wantAPIVersion: "search.grafana.app/v1"},
		{name: "omitted api version", body: `{"kind":"HybridSearchResults","items":[]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			client, err := search.NewClient(config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}, Namespace: "org-1"})
			require.NoError(t, err)
			result, err := client.Search(t.Context(), search.Query{Query: "q", Limit: 50})
			require.NoError(t, err)
			require.Equal(t, tc.wantAPIVersion, result.APIVersion)
			require.Equal(t, "HybridSearchResults", result.Kind)
			require.Equal(t, tc.wantItemsNil, result.Items == nil)
		})
	}
}

func TestClientSearchRejectsInvalidJSONResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"kind":`))
	}))
	defer server.Close()
	client, err := search.NewClient(config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}, Namespace: "org-1"})
	require.NoError(t, err)
	_, err = client.Search(t.Context(), search.Query{Query: "q", Limit: 50})
	require.ErrorContains(t, err, "decode hybrid search response")
}

func TestClientSearchPreservesServerErrorsWithoutFallback(t *testing.T) {
	for _, tc := range []struct {
		status  int
		message string
	}{
		{status: 422, message: "resources are required"},
		{status: 404, message: "route unavailable"},
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
			require.Equal(t, tc.message, status.ServerMessage)
			require.Equal(t, "hybrid search failed: HTTP "+strconv.Itoa(tc.status)+": "+tc.message, status.Message)
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

func TestClientSearchShowsBothEndsOfLongGrafanaValidationError(t *testing.T) {
	message := `query: Invalid value: "` + strings.Repeat("é", 3000) + `": must not exceed 1000 bytes`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": message, "traceID": "trace-123"})
	}))
	defer server.Close()
	client, err := search.NewClient(config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}, Namespace: "org-1"})
	require.NoError(t, err)
	_, err = client.Search(t.Context(), search.Query{Query: "q", Limit: 50})
	var status *gcxerrors.HTTPStatusError
	require.ErrorAs(t, err, &status)
	require.Equal(t, http.StatusUnprocessableEntity, status.Status)
	require.Equal(t, "application/json", status.ContentType)
	require.Equal(t, "trace-123", status.TraceID)
	require.True(t, strings.HasPrefix(status.ServerMessage, `query: Invalid value: "`))
	require.True(t, strings.HasSuffix(status.ServerMessage, `": must not exceed 1000 bytes`))
	require.Contains(t, status.ServerMessage, " ... [truncated] ... ")
	require.LessOrEqual(t, len(status.ServerMessage), 2048)
	require.True(t, utf8.ValidString(status.ServerMessage))
	require.NotContains(t, status.ServerMessage, `"message":`)
}

func TestClientSearchAcceptsExactErrorBodyLimitAndRejectsOverflow(t *testing.T) {
	for _, tc := range []struct {
		name      string
		length    int
		wantError bool
	}{
		{name: "exact limit", length: 64 << 10},
		{name: "one byte over", length: 64<<10 + 1, wantError: true},
		{name: "far over", length: 100000, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &searchReadBody{reader: strings.NewReader(strings.Repeat("a", tc.length))}
			client, err := search.NewClient(config.NamespacedRESTConfig{Config: rest.Config{
				Host: "http://example.invalid",
				WrapTransport: func(http.RoundTripper) http.RoundTripper {
					return searchRoundTripFunc(func(*http.Request) (*http.Response, error) {
						return &http.Response{StatusCode: http.StatusUnprocessableEntity, Status: "422 Unprocessable Entity", Body: body, Header: http.Header{"Content-Type": []string{"text/plain"}}}, nil
					})
				},
			}, Namespace: "org-1"})
			require.NoError(t, err)
			_, err = client.Search(t.Context(), search.Query{Query: "q", Limit: 50})
			var status *gcxerrors.HTTPStatusError
			require.ErrorAs(t, err, &status)
			require.Equal(t, 422, status.Status)
			require.Equal(t, "text/plain", status.ContentType)
			if tc.wantError {
				require.ErrorContains(t, status.Cause, "response body exceeds 65536 bytes limit")
				require.Contains(t, status.ServerMessage, "could not read hybrid search error response:")
				require.LessOrEqual(t, body.read, 65537)
			} else {
				require.NoError(t, status.Cause)
				require.Equal(t, 65536, body.read)
				require.Contains(t, status.ServerMessage, " ... [truncated] ... ")
			}
		})
	}
}

func TestClientSearchPreservesErrorReadingResponse(t *testing.T) {
	for _, cause := range []error{errors.New("fixture read failed"), context.Canceled} {
		t.Run(cause.Error(), func(t *testing.T) {
			body := &searchReadBody{reader: searchFailingReader{cause: cause}}
			client, err := search.NewClient(config.NamespacedRESTConfig{Config: rest.Config{
				Host: "http://example.invalid",
				WrapTransport: func(http.RoundTripper) http.RoundTripper {
					return searchRoundTripFunc(func(*http.Request) (*http.Response, error) {
						return &http.Response{StatusCode: http.StatusServiceUnavailable, Status: "503 Service Unavailable", Body: body, Header: http.Header{"Content-Type": []string{"application/json"}}}, nil
					})
				},
			}, Namespace: "org-1"})
			require.NoError(t, err)
			_, err = client.Search(t.Context(), search.Query{Query: "q", Limit: 50})
			var status *gcxerrors.HTTPStatusError
			require.ErrorAs(t, err, &status)
			require.ErrorIs(t, err, cause)
			require.Equal(t, 503, status.Status)
			require.Equal(t, "application/json", status.ContentType)
			require.Empty(t, status.TraceID)
			require.Contains(t, status.ServerMessage, cause.Error())
		})
	}
}

type searchFailingReader struct{ cause error }

func (r searchFailingReader) Read([]byte) (int, error) { return 0, r.cause }

func TestClientSearchBoundsTraceAndIgnoresPartialJSONFields(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "long trace", body: `{"message":"bad query","traceID":"` + strings.Repeat("é", 300) + `"}`},
		{name: "invalid message type", body: `{"traceID":"trace-123","message":42}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			client, err := search.NewClient(config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}, Namespace: "org-1"})
			require.NoError(t, err)
			_, err = client.Search(t.Context(), search.Query{Query: "q", Limit: 50})
			var status *gcxerrors.HTTPStatusError
			require.ErrorAs(t, err, &status)
			if tc.name == "long trace" {
				require.LessOrEqual(t, len(status.TraceID), 256)
				require.True(t, utf8.ValidString(status.TraceID))
				require.Contains(t, status.TraceID, " ... [truncated] ... ")
			} else {
				require.Empty(t, status.TraceID)
				require.Equal(t, tc.body, status.ServerMessage)
			}
		})
	}
}

func TestClientSearchKeepsTraceWhenJSONMessageIsEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"","traceID":"trace-only"}`))
	}))
	defer server.Close()
	client, err := search.NewClient(config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}, Namespace: "org-1"})
	require.NoError(t, err)
	_, err = client.Search(t.Context(), search.Query{Query: "q", Limit: 50})
	var status *gcxerrors.HTTPStatusError
	require.ErrorAs(t, err, &status)
	require.Equal(t, "trace-only", status.TraceID)
	require.JSONEq(t, `{"message":"","traceID":"trace-only"}`, status.ServerMessage)
}

func TestClientSearchFallsBackForEmptyAndMalformedErrorBodies(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{name: "empty", body: "", want: "422 Unprocessable Entity"},
		{name: "malformed JSON", body: `{"message":`, want: `{"message":`},
		{name: "long raw text", body: "begin " + strings.Repeat("é", 2000) + " final reason", want: " final reason"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/plain")
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			client, err := search.NewClient(config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}, Namespace: "org-1"})
			require.NoError(t, err)
			_, err = client.Search(t.Context(), search.Query{Query: "q", Limit: 50})
			var status *gcxerrors.HTTPStatusError
			require.ErrorAs(t, err, &status)
			require.Equal(t, 422, status.Status)
			require.Equal(t, "text/plain", status.ContentType)
			require.Contains(t, status.ServerMessage, tc.want)
			require.LessOrEqual(t, len(status.ServerMessage), 2048)
			require.True(t, utf8.ValidString(status.ServerMessage))
		})
	}
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
