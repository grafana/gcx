package athena

import (
	"regexp"
	"strconv"
	"strings"
	"time"
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

// GrafanaQueryResponse is the top-level wire format from Grafana's query API.
type GrafanaQueryResponse struct {
	Results map[string]GrafanaResult `json:"results"`
}

type GrafanaResult struct {
	Frames      []DataFrame `json:"frames"`
	Error       string      `json:"error,omitempty"`
	ErrorSource string      `json:"errorSource,omitempty"`
	Status      int         `json:"status,omitempty"`
}

type DataFrame struct {
	Schema DataFrameSchema `json:"schema"`
	Data   DataFrameData   `json:"data"`
}

type DataFrameSchema struct {
	Fields []DataFrameField `json:"fields"`
}

type DataFrameField struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type DataFrameData struct {
	Values [][]any `json:"values"`
}

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
