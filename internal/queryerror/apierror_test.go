package queryerror_test

import (
	"testing"

	"github.com/grafana/gcx/internal/queryerror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFromBody_ExtractsGrafanaDatasourceQueryError(t *testing.T) {
	err := queryerror.FromBody("loki", "query", 400, []byte(`{"results":{"A":{"error":"parse error at line 1, col 12: syntax error: unexpected IDENTIFIER, expecting STRING","errorSource":"downstream","status":400}}}`))

	require.NotNil(t, err)
	assert.Equal(t, "loki", err.Datasource)
	assert.Equal(t, "query", err.Operation)
	assert.Equal(t, 400, err.StatusCode)
	assert.Equal(t, "parse error at line 1, col 12: syntax error: unexpected IDENTIFIER, expecting STRING", err.Message)
	assert.Equal(t, "downstream", err.ErrorSource)
	assert.True(t, err.IsParseError())
}

func TestFromBody_FallsBackToPlainText(t *testing.T) {
	err := queryerror.FromBody("tempo", "metrics query", 500, []byte("internal error\n"))

	require.NotNil(t, err)
	assert.Equal(t, 500, err.StatusCode)
	assert.Equal(t, "internal error", err.Message)
	assert.Empty(t, err.ErrorSource)
}

func TestFromBody_TransportStatusPrecedence(t *testing.T) {
	tests := []struct {
		name          string
		transportCode int
		body          string
		wantStatus    int
		wantMessage   string
		wantSource    string
	}{
		{
			// A successful HTTP response that hides a query-level failure
			// in the envelope must surface the embedded status so callers
			// can classify the error correctly.
			name:          "2xx transport with embedded 4xx promotes embedded status",
			transportCode: 200,
			body:          `{"results":{"A":{"error":"bad query","status":400}}}`,
			wantStatus:    400,
			wantMessage:   "bad query",
		},
		{
			// Auth/proxy/gateway failures return a non-2xx transport status
			// with an unrelated downstream error object embedded. The
			// transport status is the source of truth and must not be
			// overwritten — otherwise ExitAuthFailure handling is suppressed.
			name:          "401 transport with embedded 400 preserves transport status",
			transportCode: 401,
			body:          `{"results":{"A":{"error":"forwarded downstream error","status":400}}}`,
			wantStatus:    401,
			wantMessage:   "forwarded downstream error",
		},
		{
			name:          "500 transport with embedded 400 preserves transport status",
			transportCode: 500,
			body:          `{"results":{"A":{"error":"gateway failure","status":400}}}`,
			wantStatus:    500,
			wantMessage:   "gateway failure",
		},
		{
			name:          "4xx transport with no embedded status stays on transport",
			transportCode: 404,
			body:          `{"message":"not found"}`,
			wantStatus:    404,
			wantMessage:   "not found",
		},
		{
			name:          "2xx transport with embedded status and errorSource promotes both",
			transportCode: 200,
			body:          `{"results":{"A":{"error":"parse error","errorSource":"downstream","status":400}}}`,
			wantStatus:    400,
			wantMessage:   "parse error",
			wantSource:    "downstream",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := queryerror.FromBody("prometheus", "query", tc.transportCode, []byte(tc.body))

			require.NotNil(t, err)
			assert.Equal(t, tc.wantStatus, err.StatusCode)
			assert.Equal(t, tc.wantMessage, err.Message)
			assert.Equal(t, tc.wantSource, err.ErrorSource)
		})
	}
}
