package output

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"reflect"

	"github.com/grafana/gcx/internal/format"
)

// CSVMarshaler supplies export data without terminal display formatting.
type CSVMarshaler interface {
	CSVRecords() (columns []string, rows [][]string, err error)
}

// CSV describes an export independently of terminal table formatting. Records
// returns column names and equally sized rows, including headers for empty data.
type CSV[T any] struct {
	Records func(T) (columns []string, rows [][]string, err error)
}

func (c CSV[T]) Format() format.Format { return "csv" }

func (c CSV[T]) Decode(io.Reader, any) error {
	return errors.New("csv output does not support decoding")
}

func (c CSV[T]) Encode(w io.Writer, data any) error {
	value, ok := data.(T)
	if !ok {
		return fmt.Errorf("invalid data type for csv codec: expected %v, got %T", reflect.TypeFor[T](), data)
	}
	columns, rows, err := c.Records(value)
	if err != nil {
		return err
	}
	for i, row := range rows {
		if len(row) != len(columns) {
			return fmt.Errorf("csv row %d has %d fields, expected %d", i+1, len(row), len(columns))
		}
	}
	writer := csv.NewWriter(w)
	if len(columns) > 0 {
		if err := writer.Write(columns); err != nil {
			return err
		}
	}
	return writer.WriteAll(rows)
}

// RegisterCSV opts a command into CSV only when it has an export projection.
func RegisterCSV(opts *Options) {
	opts.RegisterCustomCodec("csv", CSV[CSVMarshaler]{Records: CSVMarshaler.CSVRecords})
}
