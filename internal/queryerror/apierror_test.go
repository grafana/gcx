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
