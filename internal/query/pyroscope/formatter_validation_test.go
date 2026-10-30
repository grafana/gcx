package pyroscope_test

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/internal/query/pyroscope"
	"github.com/stretchr/testify/require"
)

func TestExtractTopFunctionsInvalidGraph(t *testing.T) {
	tests := []struct {
		name   string
		values []string
	}{
		{"malformed offset before another node", []string{"bad", "5", "0", "1", "0", "5", "5", "1"}},
		{"malformed total before another node", []string{"0", "bad", "0", "1", "0", "5", "5", "1"}},
		{"negative offset", []string{"-1", "5", "0", "1"}},
		{"negative total", []string{"0", "-1", "0", "1"}},
		{"negative self", []string{"0", "5", "-1", "1"}},
		{"self exceeds total", []string{"0", "5", "6", "1"}},
		{"offset overflow", []string{"0", "5", "0", "1", "9223372036854775807", "1", "0", "1"}},
		{"total overflow", []string{"1", "9223372036854775807", "0", "1"}},
		{"invalid name", []string{"0", "5", "0", "2"}},
		{"incomplete tuple", []string{"0", "5", "0"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fg := &pyroscope.Flamegraph{Names: []string{"total", "A"}, Total: 10, Levels: []pyroscope.Level{
				{Values: []string{"0", "10", "0", "0"}},
				{Values: tt.values},
			}}
			samples, err := pyroscope.ExtractTopFunctions(fg, 20)
			require.Error(t, err)
			require.Nil(t, samples)
			resp := &pyroscope.QueryResponse{Flamegraph: fg}
			require.Error(t, pyroscope.FormatQueryTable(&bytes.Buffer{}, resp))
			_, _, err = resp.CSVRecords()
			require.Error(t, err)
		})
	}
	t.Run("orphan", func(t *testing.T) {
		fg := &pyroscope.Flamegraph{Names: []string{"total", "A"}, Total: 10, Levels: []pyroscope.Level{
			{Values: []string{"0", "10", "0", "0"}},
			{Values: []string{"0", "5", "0", "1"}},
			{Values: []string{"5", "5", "5", "1"}},
		}}
		_, err := pyroscope.ExtractTopFunctions(fg, 20)
		require.ErrorContains(t, err, "no parent")
	})
}
