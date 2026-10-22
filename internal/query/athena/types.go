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
	out, _ := querysql.EnforceLimit(sql, limit, maxLimit, athenaBail)
	return out
}

// EnforceLimitSentinel is EnforceLimit's truncation-detecting variant: it
// injects "LIMIT eff+1" so the caller can tell whether more rows matched than
// the cap allows. See querysql.EnforceLimitSentinel for the contract.
func EnforceLimitSentinel(sql string, limit, maxLimit int) (string, int, bool) {
	return querysql.EnforceLimitSentinel(sql, limit, maxLimit, athenaBail)
}

func athenaBail(sql string) bool {
	upper := strings.ToUpper(strings.TrimSpace(sql))
	if strings.HasPrefix(upper, "SHOW") || strings.HasPrefix(upper, "DESCRIBE") || strings.HasPrefix(upper, "EXPLAIN") {
		return true
	}
	return limitBailRe.MatchString(strings.TrimRight(sql, "; \t\n"))
}
