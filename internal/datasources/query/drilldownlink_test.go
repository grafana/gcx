package query_test

import (
	"testing"

	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/stretchr/testify/assert"
)

func TestEscapePrimaryLabel(t *testing.T) {
	tests := map[string]struct {
		value string
		want  string
	}{
		"space encodes as percent-20, not plus": {"foo bar", "foo%20bar"},
		"literal plus is percent-encoded":       {"foo+bar", "foo%2Bbar"},
		"slash normalized to hyphen":            {"foo/bar", "foo-bar"},
		"backslash normalized to hyphen":        {`foo\bar`, "foo-bar"},
		"empty value":                           {"", ""},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.want, dsquery.EscapePrimaryLabel(tt.value))
		})
	}
}

func TestEncodeLabelFilter_EmptyValueUsesDrilldownSentinel(t *testing.T) {
	// Logs Drilldown's stringifyAdHocValues emits the literal `""` sentinel
	// for an empty value instead of the ad-hoc user-input prefix applied to
	// an empty string — otherwise {app=""} would decode on the Drilldown
	// side as a non-empty ad-hoc-marker value, changing the query.
	got := dsquery.EncodeLabelFilter("app", "=", "")
	assert.Equal(t, `app|=|"",`, got)
}

func TestEncodeLabelFilter_NonEmptyValueUnaffected(t *testing.T) {
	got := dsquery.EncodeLabelFilter("app", "=", "foo")
	assert.Equal(t, "app|=|__CVΩ__foo,foo", got)
}

func TestLineFilterKeyAndValue(t *testing.T) {
	tests := []struct {
		name      string
		index     int
		operator  string
		value     string
		wantKey   string
		wantValue string
	}{
		{
			name:      "literal filter uses caseSensitive,index",
			index:     0,
			operator:  "|=",
			value:     "error",
			wantKey:   "caseSensitive,0",
			wantValue: "error",
		},
		{
			name:      "regex filter without (?i) uses caseSensitive,index",
			index:     1,
			operator:  "|~",
			value:     "err.*",
			wantKey:   "caseSensitive,1",
			wantValue: "err.*",
		},
		{
			name:      "case-insensitive regex filter uses literal caseInsensitive key and strips marker",
			index:     2,
			operator:  "|~",
			value:     "(?i)error",
			wantKey:   "caseInsensitive",
			wantValue: "error",
		},
		{
			name:      "negated case-insensitive regex filter also strips marker",
			index:     0,
			operator:  "!~",
			value:     "(?i)debug",
			wantKey:   "caseInsensitive",
			wantValue: "debug",
		},
		{
			name:      "literal filter whose value happens to contain (?i) is not treated as case-insensitive",
			index:     3,
			operator:  "|=",
			value:     "(?i)literal",
			wantKey:   "caseSensitive,3",
			wantValue: "(?i)literal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotKey, gotValue := dsquery.LineFilterKeyAndValue(tt.index, tt.operator, tt.value)
			assert.Equal(t, tt.wantKey, gotKey)
			assert.Equal(t, tt.wantValue, gotValue)
		})
	}
}
