// Package arrowtable builds typed Apache Arrow record batches from query
// results and writes them as an Arrow IPC payload — the streaming format
// (io.Writer, no footer) when the destination isn't a seekable regular file,
// the file format (footer-terminated, supports random access) when it is.
package arrowtable

import (
	"fmt"
	"io"
	"math"
	"os"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// timestampType is the Arrow type used for every Timestamp column:
// nanosecond precision, UTC.
var timestampType = &arrow.TimestampType{Unit: arrow.Nanosecond, TimeZone: "UTC"} //nolint:gochecknoglobals

// Field describes one column of a Builder's schema. Use the Utf8, Float64,
// Int64, Timestamp, and Boolean constructors rather than building a Field
// literal directly.
type Field struct {
	Name     string
	Type     arrow.DataType
	Nullable bool
}

// Utf8 declares a nullable string column.
func Utf8(name string) Field {
	return Field{Name: name, Type: arrow.BinaryTypes.String, Nullable: true}
}

// Float64 declares a nullable 64-bit float column.
func Float64(name string) Field {
	return Field{Name: name, Type: arrow.PrimitiveTypes.Float64, Nullable: true}
}

// Int64 declares a nullable 64-bit integer column.
func Int64(name string) Field {
	return Field{Name: name, Type: arrow.PrimitiveTypes.Int64, Nullable: true}
}

// Timestamp declares a nullable nanosecond-precision UTC timestamp column.
func Timestamp(name string) Field {
	return Field{Name: name, Type: timestampType, Nullable: true}
}

// Boolean declares a nullable boolean column.
func Boolean(name string) Field {
	return Field{Name: name, Type: arrow.FixedWidthTypes.Boolean, Nullable: true}
}

// Builder accumulates typed rows for a fixed schema and writes them as a
// single-batch Arrow IPC payload.
type Builder struct {
	schema *arrow.Schema
	rb     *array.RecordBuilder
}

// NewBuilder creates a Builder for the given columns, in order.
func NewBuilder(fields []Field) *Builder {
	afields := make([]arrow.Field, len(fields))
	for i, f := range fields {
		afields[i] = arrow.Field{Name: f.Name, Type: f.Type, Nullable: f.Nullable}
	}
	schema := arrow.NewSchema(afields, nil)
	return &Builder{schema: schema, rb: array.NewRecordBuilder(memory.DefaultAllocator, schema)}
}

// Row appends one row. vals must supply exactly one value per field passed
// to NewBuilder, in order — every column advances one row per Row call, so a
// short or long vals slice would desync column lengths and produce a corrupt
// record; Row panics instead, since that's always a caller bug, not bad
// input data. Use nil for a value that's genuinely absent. Accepted Go types
// per column type — Utf8: string (anything else is formatted with
// fmt.Sprint); Float64/Int64: any Go numeric type; Timestamp: time.Time;
// Boolean: bool. A value that doesn't match its column's type appends null
// rather than panicking or silently truncating.
func (b *Builder) Row(vals ...any) {
	if len(vals) != len(b.rb.Fields()) {
		panic(fmt.Sprintf("arrowtable: Row got %d values, schema has %d fields", len(vals), len(b.rb.Fields())))
	}
	for i, v := range vals {
		appendValue(b.rb.Field(i), v)
	}
}

func appendValue(fb array.Builder, v any) {
	if v == nil {
		fb.AppendNull()
		return
	}

	switch bld := fb.(type) {
	case *array.StringBuilder:
		if s, ok := v.(string); ok {
			bld.Append(s)
		} else {
			bld.Append(fmt.Sprint(v))
		}
	case *array.Float64Builder:
		if f, ok := toFloat64(v); ok {
			bld.Append(f)
		} else {
			bld.AppendNull()
		}
	case *array.Int64Builder:
		if n, ok := toInt64(v); ok {
			bld.Append(n)
		} else {
			bld.AppendNull()
		}
	case *array.TimestampBuilder:
		if t, ok := v.(time.Time); ok {
			bld.AppendTime(t)
		} else {
			bld.AppendNull()
		}
	case *array.BooleanBuilder:
		if bv, ok := v.(bool); ok {
			bld.Append(bv)
		} else {
			bld.AppendNull()
		}
	default:
		fb.AppendNull()
	}
}

func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint64:
		return float64(n), true
	default:
		return 0, false
	}
}

func toInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case int:
		return int64(n), true
	case uint64:
		if n > math.MaxInt64 {
			return 0, false
		}
		return int64(n), true
	case float64:
		return int64(n), true
	default:
		return 0, false
	}
}

// Write finalizes the accumulated rows into one record batch and writes it
// as an Arrow IPC payload to w: the streaming format when w is not a
// seekable regular file (pipe, socket, terminal), the file format
// (footer-terminated, supports random access — what DuckDB's read_arrow
// wants for on-disk files) when w is a regular file. The Builder is spent
// after Write; create a new one to build another payload.
func (b *Builder) Write(w io.Writer) error {
	defer b.rb.Release()
	rec := b.rb.NewRecordBatch()
	defer rec.Release()

	if isRegularFile(w) {
		fw, err := ipc.NewFileWriter(w, ipc.WithSchema(b.schema))
		if err != nil {
			return err
		}
		if err := fw.Write(rec); err != nil {
			return err
		}
		return fw.Close()
	}

	sw := ipc.NewWriter(w, ipc.WithSchema(b.schema))
	if err := sw.Write(rec); err != nil {
		return err
	}
	return sw.Close()
}

// isRegularFile reports whether w is a seekable regular file on disk (e.g.
// shell output redirected with `>`), as opposed to a pipe, socket, or
// terminal. Anything that isn't a concrete *os.File — including the
// in-memory writers used by tests — is treated as non-regular, which
// defaults Write to the universally-valid streaming format.
func isRegularFile(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode().IsRegular()
}
