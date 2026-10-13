package faro

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/query/pinot"
	querysql "github.com/grafana/gcx/internal/query/sql"
	"golang.org/x/sync/errgroup"
)

const (
	engineSingle     = "single"
	engineMultistage = "multistage"

	// frontendQueryParallelism bounds concurrent statements in one command.
	frontendQueryParallelism = 10

	tableExceptions   = "faro_pinot_exceptions_v1"
	tableMeasurements = "faro_pinot_measurements_v1"
)

// sqlParam is one substituted placeholder. Kind decides how the value is
// rendered: sqlInt (validated base-10 integer, unquoted), sqlString (quoted
// and escaped), or sqlRaw (pre-built SQL fragment such as the filter lines).
type sqlParam struct {
	Key   string
	Value string
	Kind  sqlParamKind
	// Hidden params are substituted but not listed in the header comment.
	Hidden bool
}

type sqlParamKind int

const (
	sqlInt sqlParamKind = iota
	sqlString
	sqlRaw
)

func intParam(key string, v int64) sqlParam {
	return sqlParam{Key: key, Value: strconv.FormatInt(v, 10), Kind: sqlInt}
}

func intStringParam(key, v string) sqlParam {
	return sqlParam{Key: key, Value: v, Kind: sqlInt}
}

func stringParam(key, v string) sqlParam {
	return sqlParam{Key: key, Value: v, Kind: sqlString}
}

func rawParam(key, v string) sqlParam {
	return sqlParam{Key: key, Value: v, Kind: sqlRaw, Hidden: true}
}

// statement is one PinotQL statement a frontend command runs. SQL is built
// once by newStatement and is both what is sent to the datasource and what
// --sql prints, so the two cannot drift.
type statement struct {
	Name   string
	Table  string
	Engine string
	SQL    string
	Notes  []string
}

// newStatement substitutes every {{KEY}} placeholder in tmpl with a literal and
// prefixes a comment header naming the command line and the parameters. The
// time range is never substituted: $__timeFilter(...) is expanded by the
// datasource from the request's Start/End.
//
// The header is a block comment rather than "--" lines so the statement can be
// passed as a positional argument to gcx frontend query without the CLI
// reading it as a flag.
func newStatement(name, table, cmdline, tmpl string, params []sqlParam, notes ...string) (statement, error) {
	pairs := make([]string, 0, len(params)*2)
	var header []string
	for _, p := range params {
		var v string
		switch p.Kind {
		case sqlInt:
			n, err := pinot.FormatSQLInt(p.Value)
			if err != nil {
				return statement{}, fmt.Errorf("%s: invalid %s: %w", name, strings.ToLower(p.Key), err)
			}
			v = n
		case sqlString:
			v = "'" + pinot.EscapeSQLString(p.Value) + "'"
		case sqlRaw:
			v = p.Value
		}
		pairs = append(pairs, "{{"+p.Key+"}}", v)
		if !p.Hidden {
			header = append(header, strings.ToLower(p.Key)+"="+v)
		}
	}
	body := strings.NewReplacer(pairs...).Replace(tmpl)
	if i := strings.Index(body, "{{"); i >= 0 {
		end := strings.Index(body[i:], "}}")
		if end < 0 {
			end = len(body) - i - 2
		}
		return statement{}, fmt.Errorf("%s: unsubstituted placeholder %s", name, body[i:i+end+2])
	}

	// "*/" in a filter value would end the comment early.
	safe := strings.NewReplacer("*/", "* /")
	var b strings.Builder
	b.WriteString("/* ")
	b.WriteString(safe.Replace(cmdline))
	if len(header) > 0 {
		b.WriteString("\n   params: ")
		b.WriteString(safe.Replace(strings.Join(header, " ")))
	}
	b.WriteString(" */\n")
	b.WriteString(body)

	return statement{Name: name, Table: table, Engine: engineSingle, SQL: b.String(), Notes: notes}, nil
}

// sqlEntry is the --sql output block for one executed statement.
type sqlEntry struct {
	Name   string   `json:"name"`
	Engine string   `json:"engine"`
	SQL    string   `json:"sql"`
	Notes  []string `json:"notes,omitempty"`
}

func sqlEntries(stmts ...statement) []sqlEntry {
	out := make([]sqlEntry, len(stmts))
	for i, s := range stmts {
		out[i] = sqlEntry{Name: s.Name, Engine: s.Engine, SQL: s.SQL, Notes: s.Notes}
	}
	return out
}

// runStatements executes stmts concurrently and returns responses in input
// order.
func runStatements(ctx context.Context, client pinotQuerier, uid string, start, end time.Time, stmts []statement) ([]*querysql.QueryResponse, error) {
	out := make([]*querysql.QueryResponse, len(stmts))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(frontendQueryParallelism)
	for i, s := range stmts {
		g.Go(func() error {
			req := pinot.QueryRequest{RawSQL: s.SQL, TableName: s.Table, Start: start, End: end}
			resp, err := client.Query(gctx, uid, req)
			if isTransientPinotError(err) && gctx.Err() == nil {
				resp, err = client.Query(gctx, uid, req)
			}
			if err != nil {
				return fmt.Errorf("%s query failed: %w", s.Name, err)
			}
			out[i] = resp
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return out, nil
}

// isTransientPinotError reports a broker QUERY_EXECUTION_ERROR (code 200).
// Pinot raises it intermittently for filtered group-by aggregations (for
// example "FilteredGroupByOperator: Index N out of bounds") and a retry
// usually succeeds. Parse and unknown-column errors use other codes and are
// not retried.
func isTransientPinotError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "Code 200:")
}

// filterOpts holds the optional equality filters shared by list commands.
type filterOpts struct {
	Page        string
	Version     string
	Browser     string
	OS          string
	Environment string
}

func (f filterOpts) trimmed() filterOpts {
	return filterOpts{
		Page:        strings.TrimSpace(f.Page),
		Version:     strings.TrimSpace(f.Version),
		Browser:     strings.TrimSpace(f.Browser),
		OS:          strings.TrimSpace(f.OS),
		Environment: strings.TrimSpace(f.Environment),
	}
}

// clauses renders the filters as AND lines with escaped string literals.
func (f filterOpts) clauses() string {
	cols := []struct{ col, val string }{
		{"pageId", f.Page},
		{"appVersion", f.Version},
		{"browserName", f.Browser},
		{"osName", f.OS},
		{"appEnvironment", f.Environment},
	}
	var b strings.Builder
	for _, c := range cols {
		if c.val == "" {
			continue
		}
		fmt.Fprintf(&b, "\n  AND %s = '%s'", c.col, pinot.EscapeSQLString(c.val))
	}
	return b.String()
}

// flagArgs renders the filters back as CLI flags for next-step commands.
func (f filterOpts) flagArgs() []string {
	var out []string
	add := func(flag, v string) {
		if v != "" {
			out = append(out, flag, shellQuote(v))
		}
	}
	add("--page", f.Page)
	add("--version", f.Version)
	add("--browser", f.Browser)
	add("--os", f.OS)
	add("--environment", f.Environment)
	return out
}

// shellQuote single-quotes s when it contains anything beyond a conservative
// set of shell-safe characters.
func shellQuote(s string) string {
	safe := s != ""
	for _, r := range s {
		alnum := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
		if !alnum && !strings.ContainsRune("-_./:@+=,", r) {
			safe = false
			break
		}
	}
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// responseRows maps each row of resp to a column-name keyed map.
func responseRows(resp *querysql.QueryResponse) []map[string]any {
	if resp == nil {
		return nil
	}
	out := make([]map[string]any, 0, len(resp.Rows))
	for _, row := range resp.Rows {
		m := make(map[string]any, len(resp.Columns))
		for i, col := range resp.Columns {
			if i < len(row) {
				m[col.Name] = row[i]
			}
		}
		out = append(out, m)
	}
	return out
}

func cellString(row map[string]any, key string) string {
	v, ok := row[key]
	if !ok || v == nil {
		return ""
	}
	s := strings.TrimSpace(fmt.Sprint(v))
	if f, ok := v.(float64); ok {
		s = tsvCell(f)
	}
	if strings.EqualFold(s, "null") {
		return ""
	}
	return s
}

func cellInt(row map[string]any, key string) int64 {
	n, _ := pinotInt64(row[key])
	return n
}

// cellFloat returns the numeric value and whether it is present. Pinot
// returns aggregations over zero rows as null, NaN, or -Infinity depending on
// the function; those are reported as absent.
func cellFloat(row map[string]any, key string) (float64, bool) {
	switch v := row[key].(type) {
	case float64:
		if v != v || v > 1e300 || v < -1e300 {
			return 0, false
		}
		return v, true
	case int64:
		return float64(v), true
	case int:
		return float64(v), true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil || f != f || f > 1e300 || f < -1e300 {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}

func msTime(ms int64) *time.Time {
	if ms <= 0 {
		return nil
	}
	t := time.UnixMilli(ms).UTC()
	return &t
}
