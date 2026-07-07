package synth_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/query/synth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

const testDSUID = "sm-ds-uid"

func newTestClient(t *testing.T, handler http.HandlerFunc) *synth.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: srv.URL},
		Namespace: "default",
	}
	client, err := synth.NewClient(cfg)
	require.NoError(t, err)
	return client
}

func TestGet_BuildsProxyPathAndReturnsBody(t *testing.T) {
	var gotMethod, gotPath string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[{"id":1}]`))
	})

	resp, err := client.Get(context.Background(), testDSUID, "check/list")
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, http.MethodGet, gotMethod)
	assert.Equal(t, "/api/datasources/proxy/uid/"+testDSUID+"/sm/check/list", gotPath)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.JSONEq(t, `[{"id":1}]`, string(resp.Body))
}

// TestGet_SendsClientIdentityHeaders locks the SM-attribution contract: gcx must
// send X-Client-ID / X-Client-Version on the proxy path, because sm-api classifies
// callers by those headers (not User-Agent). Without X-Client-ID="gcx" the request
// is logged as client_type="unknown".
func TestGet_SendsClientIdentityHeaders(t *testing.T) {
	var gotClientID, gotClientVersion string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotClientID = r.Header.Get("X-Client-Id")
		gotClientVersion = r.Header.Get("X-Client-Version")
		w.WriteHeader(http.StatusOK)
	})

	_, err := client.Get(context.Background(), testDSUID, "check/list")
	require.NoError(t, err)

	assert.Equal(t, "gcx", gotClientID, "sm-api attributes callers by X-Client-ID")
	assert.NotEmpty(t, gotClientVersion, "X-Client-Version must be set so client_version is not \"unknown\"")
}

func TestPost_SendsBodyWithJSONContentType(t *testing.T) {
	var gotMethod, gotPath, gotContentType string
	var gotBody []byte
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotContentType = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"id":7}`))
	})

	resp, err := client.Post(context.Background(), testDSUID, "check/add", []byte(`{"job":"x"}`))
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, http.MethodPost, gotMethod)
	assert.Equal(t, "/api/datasources/proxy/uid/"+testDSUID+"/sm/check/add", gotPath)
	assert.Equal(t, "application/json", gotContentType)
	assert.JSONEq(t, `{"job":"x"}`, string(gotBody))
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.JSONEq(t, `{"id":7}`, string(resp.Body))
}

func TestDelete_BuildsPathWithoutBody(t *testing.T) {
	var gotMethod, gotPath, gotContentType string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	})

	resp, err := client.Delete(context.Background(), testDSUID, "check/delete/42")
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, http.MethodDelete, gotMethod)
	assert.Equal(t, "/api/datasources/proxy/uid/"+testDSUID+"/sm/check/delete/42", gotPath)
	assert.Empty(t, gotContentType, "DELETE without a body must not set Content-Type")
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestPath_LeadingSlashNormalized(t *testing.T) {
	var gotPath string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
	})

	_, err := client.Get(context.Background(), testDSUID, "/check/list")
	require.NoError(t, err)
	assert.Equal(t, "/api/datasources/proxy/uid/"+testDSUID+"/sm/check/list", gotPath,
		"a leading slash on the SM path must not produce a double slash")
}

// TestNonSuccessStatusIsReturnedNotErrored locks the layering contract: the
// transport surfaces non-2xx responses as a *Response (err == nil) so the
// dual-mode typed clients can inspect the status code and decide to fall back
// (e.g. proxy 403/404 -> direct SM API). A non-2xx must NOT be an error here.
func TestNonSuccessStatusIsReturnedNotErrored(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"plugin proxy route access denied"}`))
	})

	resp, err := client.Get(context.Background(), testDSUID, "check/list")
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Contains(t, string(resp.Body), "access denied")
}

func TestResponseTooLarge(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		chunk := make([]byte, 1024*1024) // 1 MB
		for range 51 {
			_, _ = w.Write(chunk)
		}
	})

	_, err := client.Get(context.Background(), testDSUID, "check/list")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "50 MB")
}
