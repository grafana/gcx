package pinot

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBail_apostropheInCommentsDoesNotBail(t *testing.T) {
	lineSQL := "SELECT a -- don't stop\nFROM events"
	assert.False(t, hasUnclosedBlockComment("SELECT a /* don't stop */ FROM events"))
	assert.False(t, hasUnclosedBlockComment(lineSQL))
	assert.False(t, hasTrailingLineComment(lineSQL))
	assert.False(t, bail(lineSQL))
	assert.False(t, bail("SELECT a /* don't stop */ FROM events"))
}

func TestBail_dashDashInsideBlockCommentDoesNotBail(t *testing.T) {
	mid := "SELECT a FROM events /* a -- b */ WHERE x = 1"
	leading := "SELECT /* note -- x */ a FROM events"
	assert.False(t, hasTrailingLineComment(mid))
	assert.False(t, hasTrailingLineComment(leading))
	assert.False(t, bail(mid))
	assert.False(t, bail(leading))
}

func TestHasLimitWeCannotRewrite(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want bool
	}{
		{
			name: "block comment between LIMIT and count",
			sql:  "SELECT 1 FROM events LIMIT /* note */ 5",
			want: true,
		},
		{
			name: "production-shaped query with inline LIMIT comment",
			sql:  "SELECT *  FROM faro_pinot_measurements_v1 LIMIT /* note */ 5",
			want: true,
		},
		{
			name: "LIMIT n before trailing block comment",
			sql:  "SELECT 1 FROM events LIMIT 5 /* note */",
			want: true, // shared helper also cannot match; bail may use hasLimitBeforeTrailingComment first
		},
		{
			name: "plain trailing LIMIT n",
			sql:  "SELECT 1 FROM events LIMIT 50",
			want: false,
		},
		{
			name: "no LIMIT clause",
			sql:  "SELECT * FROM events",
			want: false,
		},
		{
			name: "LIMIT only inside string literal",
			sql:  "SELECT * FROM t WHERE hint = 'LIMIT /* note */ 5'",
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, hasLimitWeCannotRewrite(tt.sql))
		})
	}
}

func TestBail_limitWithCommentBeforeCount(t *testing.T) {
	sql := "SELECT 1 FROM events LIMIT /* note */ 5"
	assert.True(t, bail(sql))
	assert.True(t, hasLimitWeCannotRewrite(sql))
}

func TestQueryLimitInfo(t *testing.T) {
	ptr := func(n int) *int { return &n }
	tests := []struct {
		name    string
		sql     string
		wantN   *int
		wantHas bool
	}{
		{name: "no LIMIT", sql: "SELECT 1 FROM t", wantN: nil, wantHas: false},
		{name: "bare LIMIT n", sql: "SELECT 1 FROM t LIMIT 150", wantN: ptr(150), wantHas: true},
		{name: "LIMIT 0", sql: "SELECT 1 FROM t LIMIT 0", wantN: ptr(0), wantHas: true},
		{name: "LIMIT with comment before n", sql: "SELECT 1 FROM t LIMIT /* note */ 5", wantN: ptr(5), wantHas: true},
		{name: "LIMIT abc", sql: "SELECT 1 FROM t LIMIT abc", wantN: nil, wantHas: true},
		{name: "LIMIT offset,count", sql: "SELECT 1 FROM t LIMIT 5,30", wantN: nil, wantHas: true},
		{name: "LIMIT offset,count large", sql: "SELECT 1 FROM t LIMIT 200,34678", wantN: nil, wantHas: true},
		{name: "LIMIT with OFFSET", sql: "SELECT 1 FROM t LIMIT 10 OFFSET 0", wantN: nil, wantHas: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, has := queryLimitInfo(tt.sql)
			assert.Equal(t, tt.wantHas, has)
			if tt.wantN == nil {
				assert.Nil(t, n)
				return
			}
			if assert.NotNil(t, n) {
				assert.Equal(t, *tt.wantN, *n)
			}
		})
	}
}
