package athena

import (
	"regexp"
	"strings"
	"time"

	querysql "github.com/grafana/gcx/internal/query/sql"
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

// StringList wraps a []string discovery result with a header for table formatting.
type StringList struct {
	Items  []string `json:"items"`
	Header string   `json:"-"`
}

var limitBailRe = regexp.MustCompile(`(?i)(\bLIMIT\s+(\d+|ALL)\s+OFFSET\b)`)

// EnforceLimit ensures the SQL has a LIMIT clause within bounds.
// If limit is 0, enforcement is disabled (pass-through).
// Athena SQL's dialect is simpler than ClickHouse — it lacks LIMIT BY,
// FORMAT, SETTINGS, etc. Only SHOW, DESCRIBE, EXPLAIN, and OFFSET are skipped.
func EnforceLimit(sql string, limit, maxLimit int) string {
	return querysql.EnforceLimit(sql, limit, maxLimit, athenaBail)
}

func athenaBail(sql string) bool {
	upper := strings.ToUpper(strings.TrimSpace(sql))
	if strings.HasPrefix(upper, "SHOW") || strings.HasPrefix(upper, "DESCRIBE") || strings.HasPrefix(upper, "EXPLAIN") {
		return true
	}
	return limitBailRe.MatchString(strings.TrimRight(sql, "; \t\n"))
}
