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
			assert.Equal(t, tc.transportCode, err.TransportStatus,
				"TransportStatus must hold the wire status even when StatusCode was promoted from the body")
		})
	}
}

// TransportStatus exists so telemetry can tell a real transport failure from
// a query failure hiding inside a 200: the 2xx-with-embedded-error case keeps
// its promoted StatusCode for rendering while TransportStatus stays 200, and
// a real 4xx stays authoritative in both fields.
func TestFromBody_PreservesTransportStatusSeparately(t *testing.T) {
	embedded := queryerror.FromBody("loki", "query", 200, []byte(`{"results":{"A":{"error":"bad query","status":400}}}`))
	assert.Equal(t, 400, embedded.StatusCode, "rendering keeps classifying on the promoted status")
	assert.Equal(t, 200, embedded.TransportStatus, "the wire said 200; telemetry must not report a failure status")

	transport := queryerror.FromBody("loki", "query", 403, []byte(`{"results":{"A":{"error":"denied","status":500}}}`))
	assert.Equal(t, 403, transport.StatusCode)
	assert.Equal(t, 403, transport.TransportStatus, "a real transport failure is authoritative over the embedded status")
}

// New is the constructor for body-level and synthesized statuses (several
// call sites default an absent embedded status to 400). Those values are not
// transport facts and must never look like one.
func TestNew_LeavesTransportStatusZero(t *testing.T) {
	err := queryerror.New("influxdb", "query", 400, "bad query", "")
	assert.Equal(t, 400, err.StatusCode)
	assert.Zero(t, err.TransportStatus,
		"a directly constructed APIError carries no transport status for telemetry to report")
}
