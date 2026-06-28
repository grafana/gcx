package athena

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/query/dataframe"
)

const (
	DatasourceType   = "grafana-athena-datasource"
	QueryFormatTable = 1
)

type QueryRequest struct {
	RawSQL                     string
	Start                      time.Time
	End                        time.Time
	Region                     string
	Catalog                    string
	Database                   string
	ResultReuseEnabled         bool
	ResultReuseMaxAgeInMinutes int
}

type QueryResponse struct {
	Columns []Column `json:"columns"`
	Rows    [][]any  `json:"rows"`
}

type Column struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// StringList wraps a []string discovery result with a header for table formatting.
type StringList struct {
	Items  []string `json:"items"`
	Header string   `json:"-"`
}

// GrafanaQueryResponse is the top-level Grafana datasource query response.
type GrafanaQueryResponse = dataframe.Response

// GrafanaResult represents a single Grafana datasource query result.
type GrafanaResult = dataframe.Result

// DataFrame represents a Grafana data frame.
type DataFrame = dataframe.Frame

// DataFrameSchema describes the structure of a Grafana data frame.
type DataFrameSchema = dataframe.Schema

// DataFrameField describes a field in a Grafana data frame.
type DataFrameField = dataframe.Field

// DataFrameData contains column-oriented Grafana data frame values.
type DataFrameData = dataframe.Data

var (
	limitClauseRe = regexp.MustCompile(`(?i)\bLIMIT\s+(\d+)\s*$`)
	limitBailRe   = regexp.MustCompile(`(?i)(\bLIMIT\s+(\d+|ALL)\s+OFFSET\b)`)
)

// EnforceLimit ensures the SQL has a LIMIT clause within bounds.
// If limit is 0, enforcement is disabled (pass-through).
// Athena SQL's dialect is simpler than ClickHouse — it lacks LIMIT BY,
// FORMAT, SETTINGS, etc. Only SHOW, DESCRIBE, and OFFSET need to be skipped.
func EnforceLimit(sql string, limit, maxLimit int) string {
	if limit == 0 {
		return sql
	}

	trimmed := strings.TrimSpace(sql)
	upper := strings.ToUpper(trimmed)
	if strings.HasPrefix(upper, "SHOW") || strings.HasPrefix(upper, "DESCRIBE") || strings.HasPrefix(upper, "EXPLAIN") {
		return sql
	}

	suffix := sql[len(strings.TrimRight(sql, "; \t\n")):]
	trimmedNoSuffix := strings.TrimRight(sql, "; \t\n")

	if limitBailRe.MatchString(trimmedNoSuffix) {
		return sql
	}

	if m := limitClauseRe.FindStringSubmatchIndex(trimmedNoSuffix); m != nil {
		existing, _ := strconv.Atoi(trimmedNoSuffix[m[2]:m[3]])
		if existing > maxLimit {
			return trimmedNoSuffix[:m[2]] + strconv.Itoa(maxLimit) + trimmedNoSuffix[m[3]:] + suffix
		}
		return sql
	}

	if limit > maxLimit {
		limit = maxLimit
	}
	return trimmedNoSuffix + " LIMIT " + strconv.Itoa(limit) + suffix
}
