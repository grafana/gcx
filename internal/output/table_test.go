package output_test

import (
	"bytes"
	"io"
	"strings"
	"testing"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type row struct {
	name string
	note string
}

func testTable() cmdio.Table[row] {
	return cmdio.Table[row]{
		Columns: []cmdio.Column[row]{
			{Header: "NAME", Cell: func(r row) string { return r.name }},
			{Header: "NOTE", Formats: []string{cmdio.FormatWide}, Cell: func(r row) string { return r.note }},
		},
	}
}

func TestTableCodecColumnsSelectByFormat(t *testing.T) {
	rows := []row{{name: "alpha", note: "first"}, {name: "beta", note: "second"}}

	tests := []struct {
		format      string
		wantHeaders []string
		wantAbsent  string
	}{
		{format: cmdio.FormatTable, wantHeaders: []string{"NAME"}, wantAbsent: "NOTE"},
		{format: cmdio.FormatWide, wantHeaders: []string{"NAME", "NOTE"}},
	}

	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			var buf bytes.Buffer
			require.NoError(t, testTable().Codec(tt.format).Encode(&buf, rows))

			out := buf.String()
			for _, h := range tt.wantHeaders {
				assert.Contains(t, out, h)
			}
			if tt.wantAbsent != "" {
				assert.NotContains(t, out, tt.wantAbsent)
			}
		})
	}
}

// Each row gets its own cell slice; TableBuilder.Row retains what it is given,
// so a shared buffer would render the last row N times.
func TestTableCodecRowsAreIndependent(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, testTable().Codec(cmdio.FormatTable).Encode(&buf, []row{{name: "alpha"}, {name: "beta"}}))

	assert.Contains(t, buf.String(), "alpha")
	assert.Contains(t, buf.String(), "beta")
}

func TestTableCodecEmpty(t *testing.T) {
	t.Run("renders headers when Empty is nil", func(t *testing.T) {
		var buf bytes.Buffer
		require.NoError(t, testTable().Codec(cmdio.FormatTable).Encode(&buf, []row{}))

		assert.Equal(t, "NAME", strings.TrimSpace(buf.String()))
	})

	t.Run("defers to Empty when set", func(t *testing.T) {
		table := testTable()
		table.Empty = func(w io.Writer) error {
			_, err := io.WriteString(w, "No rows found\n")
			return err
		}

		var buf bytes.Buffer
		require.NoError(t, table.Codec(cmdio.FormatTable).Encode(&buf, []row{}))

		assert.Equal(t, "No rows found\n", buf.String())
	})
}

func TestTableCodecFormatIsRegistrationName(t *testing.T) {
	assert.Equal(t, cmdio.FormatTable, string(testTable().Codec(cmdio.FormatTable).Format()))
	assert.Equal(t, cmdio.FormatWide, string(testTable().Codec(cmdio.FormatWide).Format()))
}

func TestTableCodecRejectsWrongPayload(t *testing.T) {
	var buf bytes.Buffer
	err := testTable().Codec(cmdio.FormatTable).Encode(&buf, "not a slice")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid data type for table codec")
}

func TestTableCodecDecodeUnsupported(t *testing.T) {
	err := testTable().Codec(cmdio.FormatTable).Decode(nil, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support decoding")
}

func TestRegisterTableRegistersWideOnlyWhenNeeded(t *testing.T) {
	t.Run("registers wide when a column is wide-only", func(t *testing.T) {
		opts := &cmdio.Options{OutputFormat: cmdio.FormatWide}
		cmdio.RegisterTable(opts, testTable())

		codec, err := opts.Codec()
		require.NoError(t, err)
		assert.Equal(t, cmdio.FormatWide, string(codec.Format()))
	})

	t.Run("skips wide when every column is shared", func(t *testing.T) {
		opts := &cmdio.Options{OutputFormat: cmdio.FormatWide}
		cmdio.RegisterTable(opts, cmdio.Table[row]{
			Columns: []cmdio.Column[row]{{Header: "NAME", Cell: func(r row) string { return r.name }}},
		})

		_, err := opts.Codec()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown output format")
	})
}

func TestOrDash(t *testing.T) {
	assert.Equal(t, "-", cmdio.OrDash(""))
	assert.Equal(t, "value", cmdio.OrDash("value"))
}
