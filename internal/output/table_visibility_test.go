package output_test

import (
	"bytes"
	"testing"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A narrow column is defined as "not wide", so it appears whatever the command
// called its narrow codec — "table" for some commands, "text" for others.
func TestTableCodecNarrowIsAnyNonWideFormat(t *testing.T) {
	table := cmdio.Table[row]{
		Columns: []cmdio.Column[row]{
			{Header: "NAME", Content: func(r row) string { return r.name }},
			{Header: "NOTE", Visible: cmdio.NarrowOnly, Content: func(r row) string { return r.note }},
			{Header: "EXTRA", Visible: cmdio.WideOnly, Content: func(row) string { return "EXTRA_CELL" }},
		},
	}

	rows := []row{{name: "alpha", note: "NARROW_CELL"}}

	for _, narrow := range []string{cmdio.FormatTable, cmdio.FormatText} {
		t.Run(narrow, func(t *testing.T) {
			var buf bytes.Buffer
			require.NoError(t, table.Codec(narrow).Encode(&buf, rows))

			assert.Contains(t, buf.String(), "NARROW_CELL")
			assert.NotContains(t, buf.String(), "EXTRA_CELL")
		})
	}

	var wide bytes.Buffer
	require.NoError(t, table.Codec(cmdio.FormatWide).Encode(&wide, rows))

	assert.Contains(t, wide.String(), "EXTRA_CELL")
	assert.NotContains(t, wide.String(), "NARROW_CELL")
}

func TestRegisterTableAsUsesGivenNarrowName(t *testing.T) {
	opts := &cmdio.Options{OutputFormat: cmdio.FormatText}
	cmdio.RegisterTableAs(opts, testTable(), cmdio.FormatText)

	codec, err := opts.Codec()
	require.NoError(t, err)
	assert.Equal(t, cmdio.FormatText, string(codec.Format()))
}
