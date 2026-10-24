package archive

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/grafana/gcx/internal/docs"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordedRequest struct {
	method string
	path   string
	body   string
	user   string
	pass   string
	ctype  string
}

func newTestClient(t *testing.T, status int, body string) (*Client, *[]recordedRequest) {
	t.Helper()
	var requests []recordedRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		user, pass, _ := r.BasicAuth()
		requests = append(requests, recordedRequest{
			method: r.Method,
			path:   r.URL.Path,
			body:   string(data),
			user:   user,
			pass:   pass,
			ctype:  r.Header.Get("Content-Type"),
		})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return NewClient(srv.URL, 42, "test-token", srv.Client()), &requests
}

const destinationJSON = `{"id":"0190a7e2-0000-7000-8000-000000000001","name":"prod-archive","description":"","type":"s3",` +
	`"storage_config":{"s3":{"bucket_name":"acme-logs","region":"us-east-1","path_prefix":"grafana"}},` +
	`"created_at":"2026-10-01T00:00:00Z","updated_at":"2026-10-01T00:00:00Z"}`

const streamJSON = `{"id":"0190a7e2-0000-7000-8000-0000000000a1","archive_destination_id":"0190a7e2-0000-7000-8000-000000000001",` +
	`"name":"payments","signal":"logs","stream_selector":"{service=\"payments\"}",` +
	`"created_at":"2026-10-01T00:00:00Z","updated_at":"2026-10-01T00:00:00Z"}`

func TestClient_Requests(t *testing.T) {
	ctx := context.Background()
	create := &DestinationCreate{
		Name: "prod-archive",
		Type: TypeS3,
		StorageConfig: StorageConfig{
			S3: &S3Config{BucketName: "acme-logs", Region: "us-east-1"},
		},
	}

	tests := []struct {
		name     string
		status   int
		response string
		call     func(*testing.T, *Client) error
		method   string
		path     string
		body     string
	}{
		{
			name:     "cell details",
			status:   http.StatusOK,
			response: `{"result":{"cloud_provider":"aws","region":"us-east-1","aws":{"arn":"arn:aws:iam::1:role/x"},"gcp":{},"azure":{}}}`,
			call: func(t *testing.T, c *Client) error {
				t.Helper()
				got, err := c.CellDetails(ctx)
				if err == nil {
					assert.Equal(t, "arn:aws:iam::1:role/x", got.Principal())
					assert.Equal(t, TypeS3, got.DestinationType())
				}
				return err
			},
			method: http.MethodGet,
			path:   "/archive-replay/api/v1/cell-details",
		},
		{
			name:     "list destinations",
			status:   http.StatusOK,
			response: `{"result":[` + destinationJSON + `]}`,
			call: func(t *testing.T, c *Client) error {
				t.Helper()
				got, err := c.ListDestinations(ctx)
				if err == nil {
					require.Len(t, got, 1)
					assert.Equal(t, "acme-logs", got[0].StorageConfig.S3.BucketName)
				}
				return err
			},
			method: http.MethodGet,
			path:   "/archive-replay/api/v1/archives",
		},
		{
			name:     "list destinations with null result",
			status:   http.StatusOK,
			response: `{"result":null}`,
			call: func(t *testing.T, c *Client) error {
				t.Helper()
				got, err := c.ListDestinations(ctx)
				if err == nil {
					assert.NotNil(t, got)
					assert.Empty(t, got)
				}
				return err
			},
			method: http.MethodGet,
			path:   "/archive-replay/api/v1/archives",
		},
		{
			name:     "get destination",
			status:   http.StatusOK,
			response: `{"result":` + destinationJSON + `}`,
			call: func(t *testing.T, c *Client) error {
				t.Helper()
				got, err := c.GetDestination(ctx, "0190a7e2-0000-7000-8000-000000000001")
				if err == nil {
					assert.Equal(t, "prod-archive", got.Name)
				}
				return err
			},
			method: http.MethodGet,
			path:   "/archive-replay/api/v1/archives/0190a7e2-0000-7000-8000-000000000001",
		},
		{
			name:     "create destination",
			status:   http.StatusCreated,
			response: `{"result":` + destinationJSON + `}`,
			call: func(_ *testing.T, c *Client) error {
				_, err := c.CreateDestination(ctx, create)
				return err
			},
			method: http.MethodPost,
			path:   "/archive-replay/api/v1/archives",
			body:   `{"name":"prod-archive","type":"s3","storage_config":{"s3":{"bucket_name":"acme-logs","region":"us-east-1"}}}`,
		},
		{
			name:     "validate destination",
			status:   http.StatusOK,
			response: `{"result":"validation passed"}`,
			call:     func(_ *testing.T, c *Client) error { return c.ValidateDestination(ctx, create) },
			method:   http.MethodPost,
			path:     "/archive-replay/api/v1/archives/validate",
			body:     `{"name":"prod-archive","type":"s3","storage_config":{"s3":{"bucket_name":"acme-logs","region":"us-east-1"}}}`,
		},
		{
			name:   "delete destination",
			status: http.StatusNoContent,
			call: func(_ *testing.T, c *Client) error {
				return c.DeleteDestination(ctx, "0190a7e2-0000-7000-8000-000000000001")
			},
			method: http.MethodDelete,
			path:   "/archive-replay/api/v1/archives/0190a7e2-0000-7000-8000-000000000001",
		},
		{
			name:     "list streams",
			status:   http.StatusOK,
			response: `{"result":[` + streamJSON + `]}`,
			call: func(t *testing.T, c *Client) error {
				t.Helper()
				got, err := c.ListStreams(ctx)
				if err == nil {
					require.Len(t, got, 1)
					assert.Equal(t, `{service="payments"}`, got[0].StreamSelector)
				}
				return err
			},
			method: http.MethodGet,
			path:   "/archive-replay/api/v1/streams",
		},
		{
			name:     "get stream",
			status:   http.StatusOK,
			response: `{"result":` + streamJSON + `}`,
			call: func(_ *testing.T, c *Client) error {
				_, err := c.GetStream(ctx, "s1")
				return err
			},
			method: http.MethodGet,
			path:   "/archive-replay/api/v1/streams/s1",
		},
		{
			name:     "create stream",
			status:   http.StatusCreated,
			response: `{"result":` + streamJSON + `}`,
			call: func(_ *testing.T, c *Client) error {
				_, err := c.CreateStream(ctx, &StreamCreate{
					ArchiveDestinationID: "d1", Name: "payments", Signal: streamSignal, StreamSelector: `{service="payments"}`,
				})
				return err
			},
			method: http.MethodPost,
			path:   "/archive-replay/api/v1/streams",
			body:   `{"archive_destination_id":"d1","name":"payments","signal":"logs","stream_selector":"{service=\"payments\"}"}`,
		},
		{
			name:     "update stream",
			status:   http.StatusOK,
			response: `{"result":` + streamJSON + `}`,
			call: func(_ *testing.T, c *Client) error {
				_, err := c.UpdateStream(ctx, "s1", &StreamUpdate{
					ArchiveDestinationID: "d1", Name: "payments", StreamSelector: `{service="payments"}`,
				})
				return err
			},
			method: http.MethodPut,
			path:   "/archive-replay/api/v1/streams/s1",
			body:   `{"archive_destination_id":"d1","name":"payments","stream_selector":"{service=\"payments\"}"}`,
		},
		{
			name:   "delete stream",
			status: http.StatusNoContent,
			call:   func(_ *testing.T, c *Client) error { return c.DeleteStream(ctx, "s1") },
			method: http.MethodDelete,
			path:   "/archive-replay/api/v1/streams/s1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client, requests := newTestClient(t, tc.status, tc.response)
			require.NoError(t, tc.call(t, client))
			require.Len(t, *requests, 1)

			req := (*requests)[0]
			assert.Equal(t, tc.method, req.method)
			assert.Equal(t, tc.path, req.path)
			assert.Equal(t, "42", req.user, "basic auth user is the logs instance ID")
			assert.Equal(t, "test-token", req.pass)
			if tc.body == "" {
				assert.Empty(t, req.body)
			} else {
				assert.JSONEq(t, tc.body, req.body)
				assert.Equal(t, "application/json", req.ctype)
			}
		})
	}
}

func TestClient_ErrorStatusReturnsAPIError(t *testing.T) {
	client, _ := newTestClient(t, http.StatusNotFound, `{"message":"not found"}`)
	_, err := client.GetDestination(context.Background(), "d1")

	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusNotFound, apiErr.HTTPStatusCode())
	assert.Equal(t, "not found", apiErr.APIUserMessage())
	assert.Equal(t, "Logs Archive", apiErr.APIServiceName())
}

func TestClient_UnreadableErrorBodyKeepsStatus(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{
			name: "oversized body",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = io.WriteString(w, strings.Repeat("x", maxResponseBodyBytes+2))
			},
		},
		{
			name: "connection closed mid-body",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Length", "1000")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = io.WriteString(w, `{"message":`)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			t.Cleanup(srv.Close)
			client := NewClient(srv.URL, 42, "test-token", srv.Client())

			_, err := client.ListDestinations(context.Background())

			var apiErr *APIError
			require.ErrorAs(t, err, &apiErr)
			assert.Equal(t, http.StatusUnauthorized, apiErr.HTTPStatusCode())
		})
	}
}

func TestAPIErrorFromResponseBody(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		body        string
		wantMessage string
		wantCode    string
		wantFields  []FieldError
	}{
		{
			name:        "destination envelope with field errors",
			status:      http.StatusForbidden,
			body:        `{"message":"storage_config: ownership verification failed","errors":[{"field":"storage_config","message":"ownership verification failed","code":"ownership_marker_missing"}]}`,
			wantMessage: "storage_config: ownership verification failed",
			wantFields:  []FieldError{{Field: "storage_config", Message: "ownership verification failed", Code: "ownership_marker_missing"}},
		},
		{
			name:        "destination envelope with top-level code",
			status:      http.StatusNotFound,
			body:        `{"code":"archive_config_not_found","message":"not found"}`,
			wantMessage: "not found",
			wantCode:    "archive_config_not_found",
		},
		{
			name:        "stream envelope",
			status:      http.StatusConflict,
			body:        `{"result":"a stream with this name already exists for this archive"}`,
			wantMessage: "a stream with this name already exists for this archive",
		},
		{
			name:        "adaptive logs envelope",
			status:      http.StatusForbidden,
			body:        `{"error":"invalid scope requested"}`,
			wantMessage: "invalid scope requested",
		},
		{
			name:        "message wins over error",
			status:      http.StatusBadRequest,
			body:        `{"message":"name: cannot be empty","error":"bad request"}`,
			wantMessage: "name: cannot be empty",
		},
		{
			name:        "non-string error is summarized",
			status:      http.StatusBadRequest,
			body:        `{"error":{"reason":"bad"}}`,
			wantMessage: `{"error":{"reason":"bad"}}`,
		},
		{
			name:        "code without a message",
			status:      http.StatusForbidden,
			body:        `{"code":"write_access_denied"}`,
			wantMessage: `{"code":"write_access_denied"}`,
			wantCode:    "write_access_denied",
		},
		{
			name:        "plain text gateway error",
			status:      http.StatusUnauthorized,
			body:        "authentication error: invalid scope requested\n",
			wantMessage: "authentication error: invalid scope requested",
		},
		{
			name:        "empty body",
			status:      http.StatusBadGateway,
			wantMessage: "Bad Gateway",
		},
		{
			name:        "empty body with a non-standard status",
			status:      599,
			wantMessage: "empty response body",
		},
		{
			name:        "unrecognized JSON",
			status:      http.StatusInternalServerError,
			body:        `{"status":"error"}`,
			wantMessage: `{"status":"error"}`,
		},
		{
			name:        "long body is truncated on a rune boundary",
			status:      http.StatusBadGateway,
			body:        "a" + strings.Repeat("é", 200),
			wantMessage: "a" + strings.Repeat("é", 119) + "...",
		},
		{
			name:        "invalid UTF-8 is replaced",
			status:      http.StatusBadGateway,
			body:        "bad \xff\xfe body",
			wantMessage: "bad \uFFFD body",
		},
		{
			name:        "lone carriage return is replaced",
			status:      http.StatusBadGateway,
			body:        "line one\rline two",
			wantMessage: "line one line two",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := apiErrorFromResponseBody(tc.status, []byte(tc.body))
			assert.Equal(t, tc.status, got.StatusCode)
			assert.Equal(t, tc.wantMessage, got.Message)
			assert.True(t, utf8.ValidString(got.Message), "message is valid UTF-8")
			assert.Equal(t, tc.wantCode, got.Code)
			assert.Equal(t, tc.wantFields, got.FieldErrors)
		})
	}
}

func TestExplainDestinationCheck(t *testing.T) {
	checkErr := func(status int, body string) error {
		return errors.Join(errors.New("failed to create archive destination"), apiErrorFromResponseBody(status, []byte(body)))
	}

	t.Run("bucket check failures become a non-auth API error with a suggestion", func(t *testing.T) {
		tests := []struct {
			code           string
			status         int
			wantSuggestion string
		}{
			{codeOwnershipMarkerMissing, http.StatusForbidden, "grafana-archive-ownership"},
			{codeReadAccessDenied, http.StatusForbidden, "Grant the principal"},
			{codeWriteAccessDenied, http.StatusForbidden, "Grant the principal"},
			{codeDestinationUnreachable, http.StatusForbidden, "bucket exists"},
			{codeConfigInvalid, http.StatusBadRequest, "cloud provider and region"},
		}
		for _, tc := range tests {
			t.Run(tc.code, func(t *testing.T) {
				body, err := json.Marshal(map[string]any{
					"message": "storage_config: check failed",
					"errors":  []map[string]string{{"field": "storage_config", "message": "check failed at tenant=42/grafana-archive-ownership", "code": tc.code}},
				})
				require.NoError(t, err)

				var detailed *gcxerrors.DetailedError
				require.ErrorAs(t, explainDestinationCheck(checkErr(tc.status, string(body))), &detailed)
				assert.Equal(t, gcxerrors.SummaryAPIError, detailed.Summary)
				assert.Nil(t, detailed.ExitCode, "a bucket problem is not a gcx credential failure")
				assert.Contains(t, detailed.Details, "Logs Archive destination check failed")
				assert.Contains(t, detailed.Details, "storage_config: check failed at tenant=42/grafana-archive-ownership")
				require.Len(t, detailed.Suggestions, 1)
				assert.Contains(t, detailed.Suggestions[0], tc.wantSuggestion)
				assert.Equal(t, docs.AdaptiveLogsArchive, detailed.DocsLink)
			})
		}
	})

	t.Run("a 403 with an unknown code gets the generic suggestion", func(t *testing.T) {
		body := `{"message":"storage_config: check failed","errors":[{"field":"storage_config","message":"check failed","code":"bucket_on_fire"}]}`

		var detailed *gcxerrors.DetailedError
		require.ErrorAs(t, explainDestinationCheck(checkErr(http.StatusForbidden, body)), &detailed)
		assert.Equal(t, gcxerrors.SummaryAPIError, detailed.Summary)
		assert.Nil(t, detailed.ExitCode, "a bucket problem is not a gcx credential failure")
		assert.Contains(t, detailed.Details, "Logs Archive destination check failed (HTTP 403)")
		assert.Equal(t, []string{unknownCheckSuggestion}, detailed.Suggestions)
	})

	t.Run("errors without a bucket check code are unchanged", func(t *testing.T) {
		for _, err := range []error{
			checkErr(http.StatusBadRequest, `{"message":"name: cannot be empty","errors":[{"field":"name","message":"cannot be empty"}]}`),
			checkErr(http.StatusBadRequest, `{"message":"storage_config: check failed","errors":[{"field":"storage_config","message":"check failed","code":"bucket_on_fire"}]}`),
			checkErr(http.StatusForbidden, "authentication error: invalid scope requested"),
			checkErr(http.StatusForbidden, `{"error":"invalid scope requested"}`),
			checkErr(http.StatusForbidden, `{"code":"permission_denied","message":"forbidden"}`),
			errors.New("network down"),
		} {
			assert.Same(t, err, explainDestinationCheck(err))
		}
	})
}
