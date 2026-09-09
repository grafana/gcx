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
		offsetSQL  = "SELECT * FROM t LIMIT 5000 OFFSET 0" //nolint:unqueryvet // LIMIT/OFFSET shape fixture
		plainSQL   = "SELECT 1 LIMIT 50"
		commaSQL   = "SELECT * FROM t LIMIT 10, 20"           //nolint:unqueryvet // LIMIT offset,count shape fixture
		optionSQL  = "SELECT * FROM t OPTION(timeoutMs=5000)" //nolint:unqueryvet // OPTION shape fixture
		commentSQL = "SELECT 1 -- keep going"
	)
	cappedWarn := pinot.LimitCappedWarning(pinot.MaxLimit)
	skipWarn := pinot.LimitSkipWarning()

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
			name:    "LIMIT offset,count stays quiet",
			expr:    commaSQL,
			limit:   100,
			notWant: skipWarn,
		},
		{
			name:  "OPTION skip warns",
			expr:  optionSQL,
			limit: 100,
			want:  skipWarn,
		},
		{
			name:    "limit 0 on OPTION stays quiet",
			expr:    optionSQL,
			limit:   0,
			notWant: skipWarn,
		},
		{
			name:  "trailing comment skip warns",
			expr:  commentSQL,
			limit: 100,
			want:  skipWarn,
		},
		{
			name:  "unclosed block comment skip warns",
			expr:  "SELECT 1 FROM t /* keep",
			limit: 100,
			want:  skipWarn,
		},
		{
			name:    "limit 0 on trailing comment stays quiet",
			expr:    commentSQL,
			limit:   0,
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
			_, capped := pinot.EnforceLimit(tt.expr, tt.limit, pinot.MaxLimit)
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
