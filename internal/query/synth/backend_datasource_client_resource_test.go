package synth_test

import (
	"context"
	"io"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCallResource_RequestShape(t *testing.T) {
	var (
		gotMethod, gotPath, gotContentType string
		gotBody                            []byte
	)

	client := newNamedClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotContentType = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"suggestions":[]}`))
	})

	res, err := client.CallResource(context.Background(), "sm-uid", http.MethodPost,
		"reliability-inbox/suggestions", []byte(`{}`))
	require.NoError(t, err)

	// Resource calls reach the plugin's CallResource handler, not QueryData, so
	// the path is the datasource resources route and not the query API.
	assert.Equal(t, http.MethodPost, gotMethod)
	assert.Equal(t, "/api/datasources/uid/sm-uid/resources/reliability-inbox/suggestions", gotPath)
	assert.JSONEq(t, `{}`, string(gotBody))
	assert.Equal(t, "application/json", gotContentType)

	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.JSONEq(t, `{"suggestions":[]}`, string(res.Body))
}

// Every outbound SM request gcx sends carries the same client identity, whichever
// route it takes; the resources route must not be the one unattributed exception.
func TestCallResource_SendsClientIdentityHeaders(t *testing.T) {
	var gotClientID, gotClientVersion string

	client := newNamedClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotClientID = r.Header.Get("X-Client-Id")
		gotClientVersion = r.Header.Get("X-Client-Version")
		w.WriteHeader(http.StatusOK)
	})

	_, err := client.CallResource(context.Background(), "sm-uid", http.MethodGet, "reliability-inbox/health", nil)
	require.NoError(t, err)

	assert.Equal(t, "gcx", gotClientID)
	assert.NotEmpty(t, gotClientVersion)
}

func TestCallResource_NoBodyOmitsContentType(t *testing.T) {
	var gotContentType string

	client := newNamedClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	})

	_, err := client.CallResource(context.Background(), "sm-uid", http.MethodGet,
		"reliability-inbox/health", nil)
	require.NoError(t, err)

	assert.Empty(t, gotContentType)
}

// A non-2xx status is the caller's to interpret (404 means "not in this region",
// 503 means "not configured"), so it comes back as data and not as an error.
func TestCallResource_NonSuccessStatusIsNotAnError(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusServiceUnavailable, http.StatusBadGateway} {
		client := newNamedClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"message":"nope"}`))
		})

		res, err := client.CallResource(context.Background(), "sm-uid", http.MethodPost,
			"reliability-inbox/suggestions", []byte(`{}`))
		require.NoError(t, err, "status %d", status)

		assert.Equal(t, status, res.StatusCode)
		assert.JSONEq(t, `{"message":"nope"}`, string(res.Body))
	}
}

// Suggestion generation is a paid call. Unlike Query, which retries a non-200
// against the legacy endpoint, a resource call must hit the server exactly once.
func TestCallResource_DoesNotRetry(t *testing.T) {
	var calls atomic.Int32

	client := newNamedClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	})

	res, err := client.CallResource(context.Background(), "sm-uid", http.MethodPost,
		"reliability-inbox/suggestions", []byte(`{}`))
	require.NoError(t, err)

	assert.Equal(t, http.StatusInternalServerError, res.StatusCode)
	assert.Equal(t, int32(1), calls.Load())
}

// A UID is one path segment. One carrying '/' or '?' must not re-route the request.
func TestCallResource_EscapesDatasourceUID(t *testing.T) {
	var gotEscapedPath, gotQuery string

	client := newNamedClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotEscapedPath = r.URL.EscapedPath()
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	})

	_, err := client.CallResource(context.Background(), "a/b?x=1", http.MethodGet, "reliability-inbox/health", nil)
	require.NoError(t, err)

	assert.Equal(t, "/api/datasources/uid/a%2Fb%3Fx=1/resources/reliability-inbox/health", gotEscapedPath)
	assert.Empty(t, gotQuery)
}

func TestCallResource_RequiresUIDAndPath(t *testing.T) {
	client := newNamedClient(t, func(http.ResponseWriter, *http.Request) {
		t.Fatal("no request should be sent")
	})

	_, err := client.CallResource(context.Background(), "", http.MethodGet, "reliability-inbox/health", nil)
	require.Error(t, err)

	_, err = client.CallResource(context.Background(), "sm-uid", http.MethodGet, "", nil)
	require.Error(t, err)
}
