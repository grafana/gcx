package arrowtable

import (
	"io"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
)

// ReadStream decodes a single-record-batch Arrow IPC stream payload (as
// Builder.Write produces for any non-regular-file destination) into plain Go
// values, for tests that verify a FormatArrow function's output without
// depending on the arrow/array API directly.
//
// Returned cell types are string, float64, int64, bool, time.Time,
// map[string]string, or nil (null) — one of the six column kinds Field's
// constructors support.
func ReadStream(r io.Reader) ([]string, [][]any, error) {
	rdr, err := ipc.NewReader(r)
	if err != nil {
		return nil, nil, err
	}
	defer rdr.Release()

	if !rdr.Next() {
		return nil, nil, rdr.Err()
	}
	rec := rdr.RecordBatch()

	headers := make([]string, rec.NumCols())
	for i := range headers {
		headers[i] = rec.ColumnName(i)
	}

	rows := make([][]any, rec.NumRows())
	for i := range rows {
		rows[i] = make([]any, rec.NumCols())
	}
	for c := range int(rec.NumCols()) {
		col := rec.Column(c)
		for i := range int(rec.NumRows()) {
			rows[i][c] = columnValue(col, i)
		}
	}

	return headers, rows, nil
}

func columnValue(col arrow.Array, i int) any {
	if col.IsNull(i) {
		return nil
	}
	switch a := col.(type) {
	case *array.String:
		return a.Value(i)
	case *array.Float64:
		return a.Value(i)
	case *array.Int64:
		return a.Value(i)
	case *array.Boolean:
		return a.Value(i)
	case *array.Timestamp:
		dt, _ := a.DataType().(*arrow.TimestampType)
		if dt == nil {
			return nil
		}
		return a.Value(i).ToTime(dt.Unit)
	case *array.Map:
		return mapValue(a, i)
	default:
		return nil
	}
}

// mapValue extracts row i of a Map(Utf8, Utf8) column as a Go map.
func mapValue(m *array.Map, i int) map[string]string {
	start, end := m.ValueOffsets(i)
	keys, kOk := m.Keys().(*array.String)
	items, iOk := m.Items().(*array.String)
	if !kOk || !iOk {
		return nil
	}
	result := make(map[string]string, end-start)
	for j := start; j < end; j++ {
		result[keys.Value(int(j))] = items.Value(int(j))
	}
	return result
}
