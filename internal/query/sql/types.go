// Package sql holds the shared building blocks for SQL-style Grafana datasources
// (ClickHouse, Athena, …) that query via Grafana's unified datasource query API
// and render row-oriented results. Dialect packages keep their own request
// construction, schema discovery, and LIMIT bail rules; the common response
// shape, table formatting, response parsing, and LIMIT clamping live here.
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
}

// Column describes a result column.
type Column struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

var limitClauseRe = regexp.MustCompile(`(?i)\bLIMIT\s+(\d+)\s*$`)

// EnforceLimit ensures the SQL has a trailing LIMIT clause within bounds.
// If limit is 0, enforcement is disabled (pass-through). The bail predicate
// lets each dialect opt out for statements where appending a LIMIT is invalid
// or unwanted (SHOW/DESCRIBE/EXPLAIN, LIMIT … OFFSET, dialect-specific clauses).
func EnforceLimit(sql string, limit, maxLimit int, bail func(string) bool) string {
	if limit == 0 {
		return sql
	}

	if bail != nil && bail(sql) {
		return sql
	}

	trimmed := strings.TrimRight(sql, "; \t\n")
	suffix := sql[len(trimmed):]

	if m := limitClauseRe.FindStringSubmatchIndex(trimmed); m != nil {
		existing, _ := strconv.Atoi(trimmed[m[2]:m[3]])
		if existing > maxLimit {
			return trimmed[:m[2]] + strconv.Itoa(maxLimit) + trimmed[m[3]:] + suffix
		}
		return sql
	}

	if limit > maxLimit {
		limit = maxLimit
	}
	return trimmed + " LIMIT " + strconv.Itoa(limit) + suffix
}
