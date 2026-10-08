package faro

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	dspinot "github.com/grafana/gcx/internal/datasources/pinot"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
)

// exploreLink builds the Explore link for a command's primary statement.
func (o *frontendOpts) exploreLink(t *frontendTarget, primary statement, subject string) dsquery.ExploreLink {
	url := dspinot.QueryExploreURL(t.cfg.GrafanaURL, dsquery.ExploreQuery{
		DatasourceUID:  t.dsUID,
		DatasourceType: t.dsType,
		Expr:           primary.SQL,
		From:           o.From,
		To:             o.To,
		OrgID:          dsquery.OrgID(t.cfgCtx),
		TableName:      primary.Table,
	})
	unavailable, failedOpen := dsquery.ExploreMessages(subject)
	return dsquery.ExploreLink{URL: url, UnavailableMsg: unavailable, FailedOpenMsg: failedOpen}
}

// rowsTableCodec renders the row slice of a frontend result as a table, then
// any --sql statements below it, so human output carries the same SQL the
// structured codecs do.
type rowsTableCodec[O any, T any] struct {
	name  string
	inner format.Codec
	rows  func(O) []T
	sql   func(O) []sqlEntry
}

func newRowsTableCodec[O any, T any](name string, t cmdio.Table[T], rows func(O) []T, sql func(O) []sqlEntry) *rowsTableCodec[O, T] {
	return &rowsTableCodec[O, T]{name: name, inner: t.Codec(name), rows: rows, sql: sql}
}

func (c *rowsTableCodec[O, T]) Format() format.Format { return format.Format(c.name) }

func (c *rowsTableCodec[O, T]) Decode(io.Reader, any) error {
	return fmt.Errorf("%s format does not support decoding", c.name)
}

func (c *rowsTableCodec[O, T]) Encode(w io.Writer, v any) error {
	out, ok := v.(O)
	if !ok {
		var want O
		return fmt.Errorf("invalid data type for %s codec: expected %T, got %T", c.name, want, v)
	}
	if err := c.inner.Encode(w, c.rows(out)); err != nil {
		return err
	}
	return writeSQLEntries(w, c.sql(out))
}

func writeSQLEntries(w io.Writer, entries []sqlEntry) error {
	for _, e := range entries {
		if _, err := fmt.Fprintf(w, "\n-- statement: %s (%s engine)\n%s\n", e.Name, e.Engine, e.SQL); err != nil {
			return err
		}
	}
	return nil
}

func formatTimePtr(t *time.Time) string {
	if t == nil {
		return "-"
	}
	return t.UTC().Format(time.RFC3339)
}

func formatInt(n int64) string { return strconv.FormatInt(n, 10) }

// oneLine collapses whitespace runs (including newlines) so a cell cannot
// break the table layout.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
