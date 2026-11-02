package k6

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProxyClient_DirectStackURLRoute(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/plugins/k6-app/resources/cloud/v3/account/me", r.URL.Path)
		writeInternalJSON(t, w, map[string]any{"token": map[string]any{"key": "personal-token"}})
	}))
	t.Cleanup(proxy.Close)

	direct := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/cloud/v6/auth", r.URL.Path)
		assert.Equal(t, "Bearer personal-token", r.Header.Get("Authorization"))
		assert.Equal(t, "https://stack.example", r.Header.Get("X-Stack-Url"))
		assert.Empty(t, r.Header.Get("X-Stack-Id"))
		writeInternalJSON(t, w, map[string]int{"stack_id": 123, "default_project_id": 456})
	}))
	t.Cleanup(direct.Close)

	client := newProxyClient(t.Context(), proxy.URL, "https://stack.example/", 123, direct.URL, proxy.Client(), direct.Client())
	response, err := client.doCloud(t.Context(), cloudRequest{
		Target: cloudTargetCloud,
		Auth:   cloudAuthDirectStackURL,
		Method: http.MethodGet,
		Path:   "/cloud/v6/auth",
	})
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, response.StatusCode)
}

func TestProxyClient_ConfiguredTargetRoutes(t *testing.T) {
	wantPaths := []string{
		"/api/plugins/k6-app/resources/cloud/cloud/v6/projects",
		"/api/plugins/k6-app/resources/logs/api/v1/query_range",
		"/api/plugins/k6-app/resources/insights/insights/api/v1/audits",
	}
	var requestIndex atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		index := int(requestIndex.Add(1)) - 1
		assert.Equal(t, wantPaths[index], r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	client := NewProxyClient(t.Context(), server.URL, server.Client())
	requests := []cloudRequest{
		{Target: cloudTargetCloud, Auth: cloudAuthConfigured, Method: http.MethodGet, Path: "/cloud/v6/projects"},
		{Target: cloudTargetLogs, Auth: cloudAuthConfigured, Method: http.MethodGet, Path: "/api/v1/query_range"},
		{Target: cloudTargetInsights, Auth: cloudAuthConfigured, Method: http.MethodGet, Path: "/insights/api/v1/audits"},
	}
	for _, request := range requests {
		_, err := client.doCloud(t.Context(), request)
		require.NoError(t, err)
	}
	assert.Equal(t, len(requests), int(requestIndex.Load()))
}

func TestProxyClient_CreateLoadTestUsesDirectMultipartRoute(t *testing.T) {
	var proxyCreateCalls atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/plugins/k6-app/resources/cloud/v3/account/me" {
			proxyCreateCalls.Add(1)
			t.Fatalf("unexpected proxy request: %s %s", r.Method, r.URL.Path)
		}
		writeInternalJSON(t, w, map[string]any{"token": map[string]any{"key": "personal-token"}})
	}))
	t.Cleanup(proxy.Close)

	direct := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/cloud/v6/projects/7/load_tests", r.URL.Path)
		assert.Equal(t, "Bearer personal-token", r.Header.Get("Authorization"))
		assert.Equal(t, "123", r.Header.Get("X-Stack-Id"))
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if !assert.NoError(t, err) {
			http.Error(w, "invalid content type", http.StatusBadRequest)
			return
		}
		assert.Equal(t, "multipart/form-data", mediaType)
		form, err := multipart.NewReader(r.Body, params["boundary"]).ReadForm(1 << 20)
		if !assert.NoError(t, err) {
			http.Error(w, "invalid multipart form", http.StatusBadRequest)
			return
		}
		assert.Equal(t, []string{"smoke"}, form.Value["name"])
		files, ok := form.File["script"]
		if !assert.True(t, ok) || !assert.Len(t, files, 1) {
			http.Error(w, "missing script", http.StatusBadRequest)
			return
		}
		file, err := files[0].Open()
		if !assert.NoError(t, err) {
			http.Error(w, "missing script", http.StatusBadRequest)
			return
		}
		defer file.Close()
		script, err := io.ReadAll(file)
		if !assert.NoError(t, err) {
			http.Error(w, "invalid script", http.StatusBadRequest)
			return
		}
		assert.Equal(t, "export default function() {}", string(script))
		w.WriteHeader(http.StatusCreated)
		writeInternalJSON(t, w, map[string]any{"id": 9, "name": "smoke", "project_id": 7})
	}))
	t.Cleanup(direct.Close)

	client := newProxyClient(t.Context(), proxy.URL, "https://stack.example", 123, direct.URL, proxy.Client(), direct.Client())
	loadTest, err := client.CreateLoadTest(t.Context(), "smoke", 7, "export default function() {}")
	require.NoError(t, err)
	assert.Equal(t, 9, loadTest.ID)
	assert.Zero(t, proxyCreateCalls.Load())
}

func TestNewProxyClient_CreateLoadTestFallsBackToProxy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/plugins/k6-app/resources/cloud/cloud/v6/projects/7/load_tests", r.URL.Path)
		assert.Contains(t, r.Header.Get("Content-Type"), "multipart/form-data")
		body, err := io.ReadAll(r.Body)
		if !assert.NoError(t, err) {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		assert.Contains(t, string(body), "smoke")
		assert.Contains(t, string(body), "export default function() {}")
		w.WriteHeader(http.StatusCreated)
		writeInternalJSON(t, w, map[string]any{"id": 9, "name": "smoke", "project_id": 7})
	}))
	t.Cleanup(server.Close)

	client := NewProxyClient(t.Context(), server.URL, server.Client())
	loadTest, err := client.CreateLoadTest(t.Context(), "smoke", 7, "export default function() {}")
	require.NoError(t, err)
	assert.Equal(t, 9, loadTest.ID)
}

func TestProxyClient_DirectMultipartRetryReplaysBodyAndHeaders(t *testing.T) {
	var tokenCalls atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		call := tokenCalls.Add(1)
		token := "old-token"
		if call == 2 {
			token = "new-token"
		}
		writeInternalJSON(t, w, map[string]any{"token": map[string]any{"key": token}})
	}))
	t.Cleanup(proxy.Close)

	var directCalls atomic.Int32
	direct := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := directCalls.Add(1)
		assert.Equal(t, "123", r.Header.Get("X-Stack-Id"))
		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if !assert.NoError(t, err) {
			http.Error(w, "invalid content type", http.StatusBadRequest)
			return
		}
		assert.Equal(t, "multipart/form-data", mediaType)
		reader := multipart.NewReader(r.Body, params["boundary"])
		form, err := reader.ReadForm(1 << 20)
		if !assert.NoError(t, err) {
			http.Error(w, "invalid multipart form", http.StatusBadRequest)
			return
		}
		assert.Equal(t, []string{"smoke"}, form.Value["name"])
		if !assert.Contains(t, form.File, "script") {
			http.Error(w, "missing script", http.StatusBadRequest)
			return
		}
		if call == 1 {
			assert.Equal(t, "Bearer old-token", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		assert.Equal(t, "Bearer new-token", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusCreated)
		writeInternalJSON(t, w, map[string]any{"id": 9, "name": "smoke", "project_id": 7})
	}))
	t.Cleanup(direct.Close)

	client := newProxyClient(t.Context(), proxy.URL, "https://stack.example", 123, direct.URL, proxy.Client(), direct.Client())
	_, err := client.CreateLoadTest(t.Context(), "smoke", 7, "export default function() {}")
	require.NoError(t, err)
	assert.Equal(t, int32(2), tokenCalls.Load())
	assert.Equal(t, int32(2), directCalls.Load())
}

func TestDirectClient_StackURLRouteReauthPreservesHeader(t *testing.T) {
	var calls atomic.Int32
	direct := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		assert.Equal(t, "/cloud/v6/auth", r.URL.Path)
		assert.Equal(t, "https://stack.example", r.Header.Get("X-Stack-Url"))
		assert.Empty(t, r.Header.Get("X-Stack-Id"))
		if call == 1 {
			assert.Equal(t, "Bearer old-token", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		assert.Equal(t, "Bearer new-token", r.Header.Get("Authorization"))
		writeInternalJSON(t, w, map[string]int{"stack_id": 123, "default_project_id": 456})
	}))
	t.Cleanup(direct.Close)

	client := newDirectClient(t.Context(), direct.URL, "https://stack.example/", direct.Client())
	client.SetCachedAuth("old-token", 42, 123)
	client.SetReauth(func(context.Context) (string, int, error) {
		return "new-token", 42, nil
	})
	response, err := client.doCloud(t.Context(), cloudRequest{
		Target: cloudTargetCloud,
		Auth:   cloudAuthDirectStackURL,
		Method: http.MethodGet,
		Path:   "/cloud/v6/auth",
	})
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, response.StatusCode)
	assert.Equal(t, int32(2), calls.Load())
}

func TestNewDirectClient_SetStackURLSupportsAuthValidation(t *testing.T) {
	direct := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/cloud/v6/auth", r.URL.Path)
		assert.Equal(t, "https://stack.example", r.Header.Get("X-Stack-Url"))
		assert.Empty(t, r.Header.Get("X-Stack-Id"))
		writeInternalJSON(t, w, map[string]int{"stack_id": 123, "default_project_id": 456})
	}))
	t.Cleanup(direct.Close)

	client := NewDirectClient(t.Context(), direct.URL, direct.Client())
	client.SetCachedAuth("personal-token", 42, 123)
	client.SetStackURL(" https://stack.example/ ")
	response, err := client.ValidateCloudAuth(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 123, response.StackID)
}

func TestBootstrapResponsesEnforceLimit(t *testing.T) {
	newClient := func() *http.Client {
		return &http.Client{Transport: cloudRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(io.LimitReader(zeroReader{}, (50<<20)+1)),
			}, nil
		})}
	}

	t.Run("direct authentication", func(t *testing.T) {
		client := NewDirectClient(t.Context(), "https://api.example.test", newClient())
		err := client.Authenticate(t.Context(), "service-token", 123)
		require.ErrorContains(t, err, "response body exceeds 50 MB limit")
	})

	t.Run("proxy organization discovery", func(t *testing.T) {
		client := NewProxyClient(t.Context(), "https://proxy.example.test", newClient())
		_, err := client.orgID(t.Context())
		require.ErrorContains(t, err, "response body exceeds 50 MB limit")
	})
}

type cloudRoundTripFunc func(*http.Request) (*http.Response, error)

func (f cloudRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestCloudOperations_ListLoadTestsByProjectUsesNestedPathAndPagination(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/plugins/k6-app/resources/cloud/cloud/v6/projects/7/load_tests", r.URL.Path)
		assert.Empty(t, r.URL.Query().Get("project_id"))
		call := calls.Add(1)
		if call == 1 {
			assert.Equal(t, "0", r.URL.Query().Get("$skip"))
			assert.Equal(t, "100", r.URL.Query().Get("$top"))
			value := make([]LoadTest, 100)
			for i := range value {
				value[i] = LoadTest{ID: i + 1, Name: "test", ProjectID: 7}
			}
			writeInternalJSON(t, w, loadTestsResponse{Value: value, Count: 101})
			return
		}
		assert.Equal(t, "100", r.URL.Query().Get("$skip"))
		writeInternalJSON(t, w, loadTestsResponse{Value: []LoadTest{{ID: 101, Name: "last", ProjectID: 7}}, Count: 101})
	}))
	t.Cleanup(server.Close)

	client := NewProxyClient(t.Context(), server.URL, server.Client())
	loadTests, err := client.ListLoadTestsByProject(t.Context(), 7)
	require.NoError(t, err)
	assert.Len(t, loadTests, 101)
	assert.Equal(t, int32(2), calls.Load())
}

func TestCloudOperations_AllowedResourceBodies(t *testing.T) {
	tests := []struct {
		name     string
		wantPath string
		wantBody string
		call     func(*cloudOperations) error
	}{
		{
			name:     "projects",
			wantPath: "/cloud/v6/load_zones/8/allowed_projects",
			wantBody: `{"value":[{"id":10},{"id":20}]}`,
			call: func(operations *cloudOperations) error {
				return operations.UpdateAllowedProjects(t.Context(), 8, []int{10, 20})
			},
		},
		{
			name:     "load zones",
			wantPath: "/cloud/v6/projects/7/allowed_load_zones",
			wantBody: `{"value":[{"id":100},{"id":200}]}`,
			call: func(operations *cloudOperations) error {
				return operations.UpdateAllowedLoadZones(t.Context(), 7, []int{100, 200})
			},
		},
		{
			name:     "empty projects",
			wantPath: "/cloud/v6/load_zones/8/allowed_projects",
			wantBody: `{"value":[]}`,
			call: func(operations *cloudOperations) error {
				return operations.UpdateAllowedProjects(t.Context(), 8, []int{})
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executor := &recordingCloudExecutor{responses: []cloudResponse{{StatusCode: http.StatusOK}}}
			operations := &cloudOperations{executor: executor}
			require.NoError(t, test.call(operations))
			require.Len(t, executor.requests, 1)
			assert.Equal(t, test.wantPath, executor.requests[0].Path)
			assert.JSONEq(t, test.wantBody, string(executor.requests[0].Body))
		})
	}
}

func TestCloudOperations_AllowedResourceValidationBeforeRequest(t *testing.T) {
	tests := []struct {
		name string
		call func(*cloudOperations) error
		want string
	}{
		{
			name: "nonpositive project ID",
			call: func(operations *cloudOperations) error {
				return operations.UpdateAllowedProjects(t.Context(), 8, []int{0})
			},
			want: "invalid allowed project ID 0",
		},
		{
			name: "too many load zone IDs",
			call: func(operations *cloudOperations) error {
				return operations.UpdateAllowedLoadZones(t.Context(), 7, make([]int, 501))
			},
			want: "got 501, maximum is 500",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executor := &recordingCloudExecutor{}
			err := test.call(&cloudOperations{executor: executor})
			require.ErrorContains(t, err, test.want)
			assert.Empty(t, executor.requests)
		})
	}
}

func TestReadCloudResponseEnforcesLimit(t *testing.T) {
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(io.LimitReader(zeroReader{}, (50<<20)+1)),
	}
	_, err := readCloudResponse(response)
	require.ErrorContains(t, err, "response body exceeds 50 MB limit")
}

type recordingCloudExecutor struct {
	requests  []cloudRequest
	responses []cloudResponse
}

func (e *recordingCloudExecutor) doCloud(_ context.Context, request cloudRequest) (cloudResponse, error) {
	e.requests = append(e.requests, request)
	response := e.responses[0]
	e.responses = e.responses[1:]
	return response, nil
}

type zeroReader struct{}

func (zeroReader) Read(buffer []byte) (int, error) {
	clear(buffer)
	return len(buffer), nil
}

func TestBuildCloudRequestCopiesHeaders(t *testing.T) {
	headers := http.Header{"X-Test": []string{"one", "two"}}
	req, err := buildCloudRequest(t.Context(), "https://example.invalid/", cloudRequest{
		Method:      http.MethodPost,
		Path:        "/path",
		Body:        []byte("body"),
		ContentType: "text/plain",
		Accept:      "application/json",
		Headers:     headers,
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"one", "two"}, req.Header.Values("X-Test"))
	assert.Equal(t, "text/plain", req.Header.Get("Content-Type"))
	assert.Equal(t, "application/json", req.Header.Get("Accept"))
	body, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	assert.True(t, bytes.Equal([]byte("body"), body))
	headers.Set("X-Test", "changed")
	assert.Equal(t, []string{"one", "two"}, req.Header.Values("X-Test"))
}

func TestCheckCloudStatusIncludesBoundedBody(t *testing.T) {
	err := checkCloudStatus(cloudResponse{StatusCode: http.StatusConflict, Body: []byte(`{"error":"conflict"}`)}, "abort run", http.StatusNoContent)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 409")
	assert.Contains(t, err.Error(), "conflict")
}

func TestDecodeCloudJSON(t *testing.T) {
	result, err := decodeCloudJSON[map[string]int](cloudResponse{Body: []byte(`{"id":7}`)})
	require.NoError(t, err)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	assert.JSONEq(t, `{"id":7}`, string(encoded))
}

func TestDirectClientLogsDomainFollowsCustomAPIDomain(t *testing.T) {
	custom := newDirectClient(t.Context(), "https://k6.example.test/", "https://stack.example", nil)
	base, err := custom.directTargetBase(cloudTargetLogs)
	require.NoError(t, err)
	assert.Equal(t, "https://k6.example.test", base)

	standard := newDirectClient(t.Context(), DefaultAPIDomain, "https://stack.example", nil)
	base, err = standard.directTargetBase(cloudTargetLogs)
	require.NoError(t, err)
	assert.Equal(t, defaultLogsDomain, base)
}

func writeInternalJSON(t *testing.T, writer http.ResponseWriter, value any) {
	t.Helper()
	writer.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(writer).Encode(value))
}
