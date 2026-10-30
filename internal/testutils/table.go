package testutils

import (
	"bytes"
	"testing"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/stretchr/testify/require"
)

func AssertTableGolden[T any](t *testing.T, table cmdio.Table[T], row T, formats ...string) {
	t.Helper()
	var zero T
	for _, name := range formats {
		t.Run(name, func(t *testing.T) {
			for _, rows := range []struct {
				name  string
				items []T
			}{
				{"populated", []T{row, zero}},
				{"empty", []T{}},
				{"nil", nil},
			} {
				t.Run(rows.name, func(t *testing.T) {
					var buf bytes.Buffer
					require.NoError(t, table.Codec(name).Encode(&buf, rows.items))
					Golden(t, t.Name(), buf.String())
				})
			}
		})
	}
}
