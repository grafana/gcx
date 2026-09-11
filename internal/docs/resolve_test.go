package docs_test

import (
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/docs"
	"github.com/grafana/mcp-doc-server/pkg/grafanadocs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testIndex(t *testing.T) *grafanadocs.Index {
	t.Helper()
	const fixture = `# Grafana documentation

## Grafana Tempo Documentation
- [TraceQL](https://grafana.com/docs/tempo/latest/traceql.md): Query traces with TraceQL.
- [Configuration](https://grafana.com/docs/tempo/latest/configuration.md): Configure Tempo with these settings.

## Grafana Loki Documentation
- [LogQL](https://grafana.com/docs/loki/latest/query.md): Query logs with LogQL.
`
	idx, err := grafanadocs.LoadIndexFromReader(strings.NewReader(fixture))
	require.NoError(t, err)
	return idx
}

func TestResolveShorthand(t *testing.T) {
	idx := testIndex(t)

	tests := []struct {
		name    string
		input   string
		product string
		wantURL string
		wantErr string
	}{
		{
			name:    "full URL is returned unchanged",
			input:   "https://grafana.com/docs/tempo/latest/traceql.md",
			wantURL: "https://grafana.com/docs/tempo/latest/traceql.md",
		},
		{
			name:    "shorthand resolves to top hit",
			input:   "traceql",
			wantURL: "https://grafana.com/docs/tempo/latest/traceql.md",
		},
		{
			name:    "product filter scopes resolution",
			input:   "query",
			product: "loki",
			wantURL: "https://grafana.com/docs/loki/latest/query.md",
		},
		{
			name:    "no match returns error with guidance",
			input:   "zzzznotathing",
			wantErr: "no matching page found",
		},
		{
			name:    "no match with product includes product in hint",
			input:   "zzzznotathing",
			product: "tempo",
			wantErr: "--product tempo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := docs.ResolveShorthand(idx, tt.input, tt.product)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantURL, got)
		})
	}
}
