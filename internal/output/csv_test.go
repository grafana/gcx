package output_test

import (
	"bytes"
	"encoding/csv"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/output"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCSV(t *testing.T) {
	tests := []struct {
		name    string
		rows    [][]string
		wantErr string
	}{
		{name: "quoted values", rows: [][]string{{"a,b", "quoted \"text\"\nnext line"}, {"", "-"}}},
		{name: "empty result"},
		{name: "short row", rows: [][]string{{"one"}}, wantErr: "csv row 1 has 1 fields, expected 2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			codec := output.CSV[[][]string]{Records: func(rows [][]string) ([]string, [][]string, error) {
				return []string{"name", "value"}, rows, nil
			}}
			var buf bytes.Buffer
			err := codec.Encode(&buf, tt.rows)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				assert.Empty(t, buf.String())
				return
			}
			require.NoError(t, err)
			records, err := csv.NewReader(&buf).ReadAll()
			require.NoError(t, err)
			assert.Equal(t, append([][]string{{"name", "value"}}, tt.rows...), records)
		})
	}
}

func TestCSVErrors(t *testing.T) {
	failure := errors.New("export failed")
	codec := output.CSV[string]{Records: func(string) ([]string, [][]string, error) {
		return nil, nil, failure
	}}
	require.ErrorIs(t, codec.Encode(io.Discard, "value"), failure)
	require.ErrorContains(t, codec.Encode(io.Discard, 1), "invalid data type")
	interfaceCodec := output.CSV[output.CSVMarshaler]{Records: output.CSVMarshaler.CSVRecords}
	require.EqualError(t, interfaceCodec.Encode(io.Discard, 1), "invalid data type for csv codec: expected output.CSVMarshaler, got int")
	require.Error(t, codec.Decode(strings.NewReader(""), nil))
	codec.Records = func(string) ([]string, [][]string, error) { return []string{"name"}, nil, nil }
	require.ErrorIs(t, codec.Encode(csvFailWriter{failure}, "value"), failure)
}

type csvFailWriter struct{ err error }

func (w csvFailWriter) Write([]byte) (int, error) { return 0, w.err }
