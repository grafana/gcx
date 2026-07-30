// Package sql holds the shared building blocks for SQL-style Grafana datasources
// (postgres, mysql, ClickHouse, Athena) that query via Grafana's unified
// datasource query API and render row-oriented results. The common response shape, table formatting,
// response parsing, LIMIT clamping, and the raw-SQL request body live here.
// Dialect packages keep their schema discovery and LIMIT bail rules, and build
// their own request body when the plugin needs more than BuildRawQueryBody
// models: a non-string "format" (ClickHouse, Athena) or extra fields such as
// Athena's connectionArgs.
package sql

import (
	"regexp"
	"strconv"
	"strings"
)

// QueryResponse holds the parsed row-oriented result of a SQL datasource query.
type QueryResponse struct {
	Columns []Column `json:"columns"`
	Rows    [][]any  `json:"rows"`

	// Notices carries warning/error-severity notices the datasource plugin
	// attached to the result (e.g. "Results have been limited to N ..."). It is
	// surfaced to the user out-of-band (stderr) and excluded from serialized
	// output so it never pollutes the `-o json`/`-o yaml` data document.
	Notices []string `json:"-"`
}

// Column describes a result column.
type Column struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

var limitClauseRe = regexp.MustCompile(`(?i)\bLIMIT\s+(\d+)\s*$`)

// EnforceLimit ensures the SQL has a trailing LIMIT clause within bounds and
// reports whether the emitted row cap is lower than what was requested — an
// existing LIMIT capped down to maxLimit, or a requested limit above maxLimit
// applied as the injected default — so callers can warn the user instead of
// truncating silently.
// If limit is 0, enforcement is disabled (pass-through). The bail predicate
// lets each dialect opt out for statements where appending a LIMIT is invalid
// or unwanted (SHOW/DESCRIBE/EXPLAIN, LIMIT … OFFSET, dialect-specific clauses).
func EnforceLimit(sql string, limit, maxLimit int, bail func(string) bool) (string, bool) {
	if limit == 0 {
		return sql, false
	}

	if bail != nil && bail(sql) {
		return sql, false
	}

	trimmed := strings.TrimRight(sql, "; \t\n")
	suffix := sql[len(trimmed):]

	if m := limitClauseRe.FindStringSubmatchIndex(trimmed); m != nil {
		existing, _ := strconv.Atoi(trimmed[m[2]:m[3]])
		if existing > maxLimit {
			return trimmed[:m[2]] + strconv.Itoa(maxLimit) + trimmed[m[3]:] + suffix, true
		}
		return sql, false
	}

	capped := limit > maxLimit
	if capped {
		limit = maxLimit
	}
	return trimmed + " LIMIT " + strconv.Itoa(limit) + suffix, capped
}
