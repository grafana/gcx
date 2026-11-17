package output

import (
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/format"
)

// TableAdapter adapts payloads that do not match the []T shape expected by
// Table[T]. P is the command's payload and T is one table row. Row extraction
// happens only in the adapted codec; JSON/YAML keep the complete payload.
//
// A wrapped list extracts its items while preserving the wrapper for JSON/YAML:
//
//	type Item struct{ Name string }
//	type Result struct {
//		Items []Item
//		Total int
//	}
//	table := Table[Item]{Columns: []Column[Item]{
//		{Header: "NAME", Content: func(item Item) string { return item.Name }},
//	}}
//	wrapped := TableAdapter[Result, Item]{
//		Table: table,
//		Rows:  func(result Result) ([]Item, error) { return result.Items, nil },
//	}
//
// A singleton reuses the same table with one row, while JSON/YAML keep an object:
//
//	singleton := TableAdapter[*Item, Item]{
//		Table: table,
//		Rows:  func(item *Item) ([]Item, error) { return []Item{*item}, nil },
//	}
//
// Register wrapped.Codec("table") or singleton.Codec("table") with the output
// options, and pass the original payload to Options.Encode for every format.
type TableAdapter[P, T any] struct {
	Table Table[T]
	Rows  func(P) ([]T, error)
}

func (a TableAdapter[P, T]) Codec(name string) format.Codec { //nolint:ireturn // codec registration requires the interface
	return &adaptedTableCodec[P, T]{Codec: a.Table.Codec(name), rows: a.Rows}
}

type adaptedTableCodec[P, T any] struct {
	format.Codec

	rows func(P) ([]T, error)
}

func (c *adaptedTableCodec[P, T]) Encode(w io.Writer, v any) error {
	payload, ok := v.(P)
	if !ok {
		return fmt.Errorf("invalid data type for %s codec: expected %T, got %T", c.Format(), payload, v)
	}
	rows, err := c.rows(payload)
	if err != nil {
		return err
	}
	return c.Codec.Encode(w, rows)
}
