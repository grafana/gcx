// Package crudcmd provides shared building blocks for the hand-copied
// list/get/create/update/delete (and pull/push) cobra command scaffolding
// that recurs across the Cloud provider tier (internal/providers/*). It
// complements internal/resources/adapter.TypedCRUD[T], which serves the same
// deduplication purpose for the K8s resource tier — this package targets the
// per-provider CLI layer instead: opts structs, table codecs, delete
// confirmation, and file-based create/update input.
//
// Per-resource files keep their own named types (e.g. a package-level
// TableCodec struct) and delegate the mechanical parts — type assertion,
// table construction, the "does not support decoding" stub — to the
// generics here, passing back only the column headers and per-row mapping
// that are genuinely specific to that resource.
package crudcmd

import (
	"errors"
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/style"
)

// ErrTableDecode is returned by every table-format codec's Decode method —
// table output is display-only and does not support round-tripping.
var ErrTableDecode = errors.New("table format does not support decoding")

// TableDecode is a ready-to-use Decode implementation for table-format codecs.
func TableDecode(io.Reader, any) error {
	return ErrTableDecode
}

// WideFormat returns "wide" when wide is true, otherwise "table". Use it to
// implement a codec's Format() method from a Wide bool field.
func WideFormat(wide bool) format.Format {
	if wide {
		return "wide"
	}
	return "table"
}

// EncodeTable type-asserts v to []T, builds a table with the given headers,
// appends one row per item via row, and renders it to w. On a type mismatch
// it returns an error naming typeName (the bare type name, e.g.
// "HookRuleDefinition") to match the message shape every hand-written table
// codec used: "invalid data type for table codec: expected []T".
func EncodeTable[T any](w io.Writer, v any, typeName string, headers []string, row func(*style.TableBuilder, T)) error {
	items, ok := v.([]T)
	if !ok {
		return fmt.Errorf("invalid data type for table codec: expected []%s", typeName)
	}
	t := style.NewTable(headers...)
	for _, item := range items {
		row(t, item)
	}
	return t.Render(w)
}

// EncodeItem type-asserts v to *T and renders a single-item detail table.
// Used by "get"-style codecs that operate on one object rather than a slice.
func EncodeItem[T any](w io.Writer, v any, typeName string, headers []string, row func(*style.TableBuilder, T)) error {
	item, ok := v.(*T)
	if !ok {
		return fmt.Errorf("invalid data type for table codec: expected *%s", typeName)
	}
	t := style.NewTable(headers...)
	row(t, *item)
	return t.Render(w)
}
