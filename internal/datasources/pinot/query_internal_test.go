package pinot

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/internal/query/pinot"
	"github.com/stretchr/testify/assert"
)

func TestWarnLimitEnforcement(t *testing.T) {
	const (
		unionSQL   = "SELECT 1 FROM a UNION SELECT 2 FROM b LIMIT 2000"
		offsetSQL  = "SELECT * FROM t LIMIT 5000 OFFSET 0"
		plainSQL   = "SELECT 1 LIMIT 50"
		cappedWarn = "LIMIT in query exceeds the maximum of 1000 and was capped; use --limit 0 to disable enforcement"
		skipWarn   = "query uses UNION or OFFSET, so --limit was not applied; the SQL was sent unchanged. Use --limit 0 to disable this warning"
	)

	tests := []struct {
		name    string
		expr    string
		limit   int
		want    string
		notWant string
	}{
		{
			name:  "union skip warns",
			expr:  unionSQL,
			limit: 100,
			want:  skipWarn,
		},
		{
			name:  "offset skip warns",
			expr:  offsetSQL,
			limit: 100,
			want:  skipWarn,
		},
		{
			name:    "limit 0 on union stays quiet",
			expr:    unionSQL,
			limit:   0,
			notWant: skipWarn,
		},
		{
			name:    "plain select under cap stays quiet",
			expr:    plainSQL,
			limit:   100,
			notWant: skipWarn,
		},
		{
			name:  "capped trailing limit warns about the ceiling",
			expr:  "SELECT 1 LIMIT 5000",
			limit: 100,
			want:  cappedWarn,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, capped := pinot.EnforceLimit(tt.expr, tt.limit, maxLimit)
			var stderr bytes.Buffer
			warnLimitEnforcement(&stderr, tt.expr, capped, tt.limit)
			got := stderr.String()
			if tt.want != "" {
				assert.Contains(t, got, tt.want)
			}
			if tt.notWant != "" {
				assert.NotContains(t, got, tt.notWant)
				assert.Empty(t, got)
			}
		})
	}
}
