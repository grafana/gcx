package output

import (
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/style"
)

// Format names a table declaration is registered under.
const (
	FormatTable = "table"
	FormatWide  = "wide"
)

// Column is one column of a Table. TableBuilder cells are strings, so Content
// does the formatting for its own column.
type Column[T any] struct {
	Header string

	// WideOnly hides the column from "table", leaving it to "wide".
	WideOnly bool

	Content func(T) string
}

func (c Column[T]) visibleIn(name string) bool {
	return !c.WideOnly || name == FormatWide
}

// Table declares how a []T renders. One declaration serves both "table" and
// "wide" — the wide columns are the narrow ones plus those marked WideOnly,
// so the two renderings cannot drift apart.
type Table[T any] struct {
	Columns []Column[T]

	// Empty renders a zero-length list. When nil, an empty list renders as
	// headers with no rows.
	Empty func(io.Writer) error
}

// Codec returns the encode-only codec rendering t under name. Format reports
// name, so one Table backs every registration it is given to.
func (t Table[T]) Codec(name string) format.Codec { //nolint:ireturn // codec registration requires the interface
	return &tableCodec[T]{name: name, table: t}
}

// RegisterTable registers t under "table", and additionally under "wide" when
// some column is wide-only.
func RegisterTable[T any](opts *Options, t Table[T]) {
	opts.RegisterCustomCodec(FormatTable, t.Codec(FormatTable))

	for _, c := range t.Columns {
		if c.WideOnly {
			opts.RegisterCustomCodec(FormatWide, t.Codec(FormatWide))
			return
		}
	}
}

type tableCodec[T any] struct {
	name  string
	table Table[T]
}

func (c *tableCodec[T]) Format() format.Format { return format.Format(c.name) }

func (c *tableCodec[T]) Decode(io.Reader, any) error {
	return fmt.Errorf("%s format does not support decoding", c.name)
}

func (c *tableCodec[T]) Encode(w io.Writer, v any) error {
	rows, ok := v.([]T)
	if !ok {
		return fmt.Errorf("invalid data type for %s codec: expected %T, got %T", c.name, rows, v)
	}

	if len(rows) == 0 && c.table.Empty != nil {
		return c.table.Empty(w)
	}

	cols := make([]Column[T], 0, len(c.table.Columns))
	headers := make([]string, 0, len(c.table.Columns))

	for _, col := range c.table.Columns {
		if col.visibleIn(c.name) {
			cols = append(cols, col)
			headers = append(headers, col.Header)
		}
	}

	tb := style.NewTable(headers...)

	for _, row := range rows {
		// A fresh slice per row: TableBuilder.Row retains what it is given.
		cells := make([]string, len(cols))
		for i, col := range cols {
			cells[i] = col.Content(row)
		}
		tb.Row(cells...)
	}

	return tb.Render(w)
}
