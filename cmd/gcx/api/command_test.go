//nolint:testpackage // white-box testing internal functions
package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/testutils"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveBody_EmptyData(t *testing.T) {
	cmd := &cobra.Command{}
	body, err := resolveBody(cmd, "")
	require.NoError(t, err)
	assert.Equal(t, http.NoBody, body)
}

func TestResolveBody_DirectString(t *testing.T) {
	cmd := &cobra.Command{}
	data := `{"title":"test"}`
	body, err := resolveBody(cmd, data)
	require.NoError(t, err)

	content, err := io.ReadAll(body)
	require.NoError(t, err)
	assert.Equal(t, data, string(content))
}

func TestResolveBody_FromFile(t *testing.T) {
	content := `{"dashboard":{"title":"My Dashboard"}}`
	filePath := testutils.CreateTempFile(t, content)

	cmd := &cobra.Command{}
	body, err := resolveBody(cmd, "@"+filePath)
	require.NoError(t, err)

	result, err := io.ReadAll(body)
	require.NoError(t, err)
	assert.Equal(t, content, string(result))
}

func TestResolveBody_FromStdin(t *testing.T) {
	content := `{"name":"test"}`
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader(content))

	body, err := resolveBody(cmd, "@-")
	require.NoError(t, err)

	result, err := io.ReadAll(body)
	require.NoError(t, err)
	assert.Equal(t, content, string(result))
}

func TestResolveBody_FileNotFound(t *testing.T) {
	cmd := &cobra.Command{}
	_, err := resolveBody(cmd, "@/nonexistent/file.json")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read file")
}

func TestOutputResponse_JSONSuccess(t *testing.T) {
	jsonData := `{"status":"ok","version":"10.0.0"}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(jsonData)),
	}

	var output bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&output)

	opts := &apiOpts{}
	opts.IO.DefaultFormat("json")
	opts.IO.OutputFormat = "json"

	err := outputResponse(cmd, opts, resp)
	require.NoError(t, err)

	result := output.String()
	assert.Contains(t, result, "status")
	assert.Contains(t, result, "ok")
}

func TestOutputResponse_JSONAsYAML(t *testing.T) {
	jsonData := `{"status":"ok","version":"10.0.0"}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(jsonData)),
	}

	var output bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&output)

	opts := &apiOpts{}
	opts.IO.DefaultFormat("yaml")
	opts.IO.OutputFormat = "yaml"

	err := outputResponse(cmd, opts, resp)
	require.NoError(t, err)

	result := output.String()
	assert.Contains(t, result, "status: ok")
	assert.Contains(t, result, "version:")
}

func TestOutputResponse_NonJSONRawOutput(t *testing.T) {
	htmlData := `<html><body>Hello</body></html>`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(htmlData)),
	}

	var output bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&output)

	opts := &apiOpts{}
	opts.IO.DefaultFormat("json")

	err := outputResponse(cmd, opts, resp)
	require.NoError(t, err)
	assert.Equal(t, htmlData, output.String())
}

func TestOutputResponse_ErrorWithBody(t *testing.T) {
	errorData := `{"error":"not found"}`
	resp := &http.Response{
		StatusCode: http.StatusNotFound,
		Body:       io.NopCloser(strings.NewReader(errorData)),
	}

	cmd := &cobra.Command{}
	opts := &apiOpts{}

	err := outputResponse(cmd, opts, resp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 404")
	assert.Contains(t, err.Error(), "not found")
}

func TestOutputResponse_ErrorWithoutBody(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusNotFound,
		Body:       io.NopCloser(strings.NewReader("")),
	}

	cmd := &cobra.Command{}
	opts := &apiOpts{}

	err := outputResponse(cmd, opts, resp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 404")
	assert.Contains(t, err.Error(), "Not Found")
}

func TestDoRequest_ContentTypeOnlyWithBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType := r.Header.Get("Content-Type")
		switch r.Method {
		case http.MethodGet:
			assert.Empty(t, contentType, "GET request should not have Content-Type header")
		case http.MethodPost:
			assert.Equal(t, "application/json", contentType, "POST request should have Content-Type: application/json")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	tests := []struct {
		name   string
		method string
		body   io.Reader
	}{
		{
			name:   "GET with no body",
			method: "GET",
			body:   http.NoBody,
		},
		{
			name:   "POST with body",
			method: "POST",
			body:   strings.NewReader(`{"test":"data"}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequestWithContext(context.Background(), tt.method, server.URL, tt.body)
			require.NoError(t, err)

			if tt.body != http.NoBody {
				req.Header.Set("Content-Type", "application/json")
			}

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusOK, resp.StatusCode)
		})
	}
}

func TestApiOpts_EffectiveMethod(t *testing.T) {
	tests := []struct {
		name     string
		method   string
		data     string
		expected string
	}{
		{
			name:     "explicit GET",
			method:   "GET",
			data:     "",
			expected: "GET",
		},
		{
			name:     "explicit POST",
			method:   "POST",
			data:     "",
			expected: "POST",
		},
		{
			name:     "data implies POST",
			method:   "",
			data:     `{"test":"data"}`,
			expected: "POST",
		},
		{
			name:     "default is GET",
			method:   "",
			data:     "",
			expected: "GET",
		},
		{
			name:     "explicit method overrides data",
			method:   "PUT",
			data:     `{"test":"data"}`,
			expected: "PUT",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &apiOpts{
				Method: tt.method,
				Data:   tt.data,
			}
			assert.Equal(t, tt.expected, opts.effectiveMethod())
		})
	}
}

func TestApiOpts_Validate(t *testing.T) {
	tests := []struct {
		name      string
		method    string
		expectErr bool
	}{
		{name: "valid GET", method: "GET", expectErr: false},
		{name: "valid POST", method: "POST", expectErr: false},
		{name: "valid PUT", method: "PUT", expectErr: false},
		{name: "valid PATCH", method: "PATCH", expectErr: false},
		{name: "valid DELETE", method: "DELETE", expectErr: false},
		{name: "valid HEAD", method: "HEAD", expectErr: false},
		{name: "valid OPTIONS", method: "OPTIONS", expectErr: false},
		{name: "valid TRACE", method: "TRACE", expectErr: false},
		{name: "lowercase method", method: "get", expectErr: false},
		{name: "invalid method", method: "INVALID", expectErr: true},
		{name: "empty method", method: "", expectErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &apiOpts{Method: tt.method}
			opts.IO.DefaultFormat("json")
			opts.IO.OutputFormat = "json"

			err := opts.Validate()
			if tt.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
