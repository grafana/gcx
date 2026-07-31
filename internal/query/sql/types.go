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
	"fmt"
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

// EffectiveLimit clamps a requested --limit to maxLimit. It returns 0 when
// limit <= 0, meaning row-count enforcement is disabled.
func EffectiveLimit(limit, maxLimit int) int {
	if limit <= 0 {
		return 0
	}
	if limit > maxLimit {
		return maxLimit
	}
	return limit
}

// EnforceLimitSentinel injects "LIMIT eff+1" so the caller can detect
// truncation, where eff = min(limit, maxLimit). It returns the SQL to execute,
// the effective row cap to display to the user (eff), and whether gcx injected
// a fresh sentinel cap. When capped is true, run the query and pass the
// response to (*QueryResponse).Truncate(eff): if it reports rows were
// dropped, warn the user with TruncationHint. When capped is false
// (enforcement disabled, statement bailed, or a user-supplied LIMIT was
// respected) the caller must not truncate or warn — the row count already
// reflects the user's own intent.
//
// Deliberately independent of EnforceLimit rather than sharing its internals:
// the two report a fresh-injection bool with different meanings (EnforceLimit's
// covers an existing-LIMIT clamp too; this one only covers a fresh sentinel
// inject), and EnforceLimit's callers (mysql, postgres) already ship on the
// simpler contract. Coupling them through one low-level helper would risk
// silently changing that shipped behavior for an unrelated caller's benefit.
func EnforceLimitSentinel(sql string, limit, maxLimit int, bail func(string) bool) (string, int, bool) {
	eff := EffectiveLimit(limit, maxLimit)
	if eff == 0 {
		return sql, 0, false
	}
	if bail != nil && bail(sql) {
		return sql, eff, false
	}

	trimmed := strings.TrimRight(sql, "; \t\n")
	suffix := sql[len(trimmed):]

	if m := limitClauseRe.FindStringSubmatchIndex(trimmed); m != nil {
		existing, _ := strconv.Atoi(trimmed[m[2]:m[3]])
		if existing > maxLimit {
			return trimmed[:m[2]] + strconv.Itoa(maxLimit) + trimmed[m[3]:] + suffix, eff, false
		}
		return sql, eff, false
	}

	return trimmed + " LIMIT " + strconv.Itoa(eff+1) + suffix, eff, true
}

// Truncate drops rows beyond eff (the sentinel and any surplus fetched via an
// eff+1 cap) and reports whether any rows were dropped, i.e. more rows matched
// than are being shown. eff <= 0 is a no-op.
func (r *QueryResponse) Truncate(eff int) bool {
	if eff <= 0 || len(r.Rows) <= eff {
		return false
	}
	r.Rows = r.Rows[:eff]
	return true
}

// TruncationHint is the stderr message shown when gcx's row cap dropped rows.
// It is dialect-agnostic (applies to both LIMIT and TOP datasources).
func TruncationHint(shown, maxLimit int) string {
	return fmt.Sprintf("showing the first %d rows; more rows match — raise --limit (max %d), pass --limit 0 to remove the cap, or add your own row limit to the query", shown, maxLimit)
}
