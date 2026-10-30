package pinot

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/internal/query/pinot"
	"github.com/stretchr/testify/assert"
)

func TestWarnLimitEnforcement(t *testing.T) {
	const (
		unionSQL     = "SELECT 1 FROM a UNION SELECT 2 FROM b"
		unionHighSQL = "SELECT 1 FROM a UNION SELECT 2 FROM b LIMIT 2000"
		offsetSQL    = "SELECT * FROM t LIMIT 5000 OFFSET 0" //nolint:unqueryvet // LIMIT/OFFSET shape fixture
		plainSQL     = "SELECT 1 LIMIT 50"
		commaSQL     = "SELECT * FROM t LIMIT 10, 20"           //nolint:unqueryvet // LIMIT offset,count shape fixture
		optionSQL    = "SELECT * FROM t OPTION(timeoutMs=5000)" //nolint:unqueryvet // OPTION shape fixture
		commentSQL   = "SELECT 1 -- keep going"
		alreadyHas   = "already has a LIMIT"
		notAppended  = "not appended"
		unsafeHigh   = "not safe to modify"
	)

	tests := []struct {
		name     string
		expr     string
		limit    int
		limitSet bool
		want     string
		notWant  string
	}{
		{
			name:     "union warns not appended when --limit set",
			expr:     unionSQL,
			limit:    100,
			limitSet: true,
			want:     notAppended,
		},
		{
			name:    "union stays quiet when --limit omitted",
			expr:    unionSQL,
			limit:   100,
			notWant: notAppended,
		},
		{
			name:     "offset with high LIMIT already has LIMIT when --limit set",
			expr:     offsetSQL,
			limit:    100,
			limitSet: true,
			want:     alreadyHas,
		},
		{
			name:  "union with high LIMIT warns unsafe when --limit omitted",
			expr:  unionHighSQL,
			limit: 100,
			want:  unsafeHigh,
		},
		{
			name:    "limit 0 on union stays quiet",
			expr:    unionSQL,
			limit:   0,
			notWant: notAppended,
		},
		{
			name:    "plain select under cap stays quiet",
			expr:    plainSQL,
			limit:   100,
			notWant: notAppended,
		},
		{
			name:     "LIMIT offset,count already has LIMIT when --limit set",
			expr:     commaSQL,
			limit:    100,
			limitSet: true,
			want:     alreadyHas,
		},
		{
			name:     "OPTION warns not appended when --limit set",
			expr:     optionSQL,
			limit:    100,
			limitSet: true,
			want:     notAppended,
		},
		{
			name:    "limit 0 on OPTION stays quiet",
			expr:    optionSQL,
			limit:   0,
			notWant: notAppended,
		},
		{
			name:     "trailing comment warns not appended when --limit set",
			expr:     commentSQL,
			limit:    100,
			limitSet: true,
			want:     notAppended,
		},
		{
			name:     "unclosed block comment warns not appended when --limit set",
			expr:     "SELECT 1 FROM t /* keep",
			limit:    100,
			limitSet: true,
			want:     notAppended,
		},
		{
			name:    "inline LIMIT comment stays quiet when --limit omitted",
			expr:    "SELECT *  FROM faro_pinot_measurements_v1 LIMIT /* note */ 5",
			limit:   100,
			notWant: notAppended,
		},
		{
			name:     "inline LIMIT comment already has LIMIT when --limit set",
			expr:     "SELECT *  FROM faro_pinot_measurements_v1 LIMIT /* note */ 5",
			limit:    100,
			limitSet: true,
			want:     alreadyHas,
		},
		{
			name:    "limit 0 on trailing comment stays quiet",
			expr:    commentSQL,
			limit:   0,
			notWant: notAppended,
		},
		{
			name:  "capped trailing limit warns about reduction",
			expr:  "SELECT 1 LIMIT 5000",
			limit: 100,
			want:  "LIMIT reduced to 1000",
		},
		{
			name:  "append omitted --limit uses default wording",
			expr:  "SELECT 1 FROM events",
			limit: 100,
			want:  "appended default LIMIT 100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, capped := pinot.EnforceLimit(tt.expr, tt.limit, pinot.MaxLimit)
			var stderr bytes.Buffer
			warnLimitEnforcement(&stderr, tt.expr, sql, capped, tt.limit, tt.limitSet)
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
