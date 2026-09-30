package output_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type pageRow struct {
	UID  string `json:"uid"`
	Name string `json:"name"`
}

func pageRows(n int) []pageRow {
	rows := make([]pageRow, n)
	for i := range rows {
		rows[i] = pageRow{UID: fmt.Sprintf("uid-%d", i), Name: fmt.Sprintf("name-%d", i)}
	}
	return rows
}

func pageRowTable() cmdio.Table[pageRow] {
	return cmdio.Table[pageRow]{Columns: []cmdio.Column[pageRow]{
		{Header: "UID", Content: func(r pageRow) string { return r.UID }},
		{Header: "NAME", Content: func(r pageRow) string { return r.Name }},
	}}
}

// encodeListWithFlags binds the output flags with a table default, sets the
// given flags, and writes rows through EncodeList.
func encodeListWithFlags(t *testing.T, flagValues map[string]string, rows []pageRow, meta *cmdio.ListMeta) string {
	t.Helper()
	opts := &cmdio.Options{}
	cmdio.RegisterTable(opts, pageRowTable())
	opts.DefaultFormat("table")
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	opts.BindFlags(flags)
	for name, value := range flagValues {
		require.NoError(t, flags.Set(name, value))
	}
	require.NoError(t, opts.Validate())

	var buf bytes.Buffer
	require.NoError(t, cmdio.EncodeList(opts, &buf, rows, meta))
	return buf.String()
}

func truncatedMeta() *cmdio.ListMeta {
	total := 5
	return &cmdio.ListMeta{Truncated: true, Returned: 2, Total: &total, Continue: "gcx x list --limit 0"}
}

// TestEncodeListShapes checks the output of EncodeList for each format. The
// structured formats get the envelope always. list_meta is present only for a
// truncated page. The table gets the bare items.
func TestEncodeListShapes(t *testing.T) {
	tests := []struct {
		name  string
		flags map[string]string
		rows  []pageRow
		meta  *cmdio.ListMeta
		want  string
		// match is "json" (JSONEq), "yaml" (YAMLEq), or "exact".
		match string
	}{
		{
			name:  "json truncated",
			flags: map[string]string{"output": "json"},
			rows:  pageRows(2),
			meta:  truncatedMeta(),
			want: `{"items":[{"uid":"uid-0","name":"name-0"},{"uid":"uid-1","name":"name-1"}],` +
				`"list_meta":{"truncated":true,"returned":2,"total":5,"continue":"gcx x list --limit 0"}}`,
			match: "json",
		},
		{
			name:  "json complete has no list_meta",
			flags: map[string]string{"output": "json"},
			rows:  pageRows(1),
			want:  `{"items":[{"uid":"uid-0","name":"name-0"}]}`,
			match: "json",
		},
		{
			name:  "json empty has an empty items array",
			flags: map[string]string{"output": "json"},
			rows:  nil,
			want:  `{"items":[]}`,
			match: "json",
		},
		{
			name:  "agents truncated",
			flags: map[string]string{"output": "agents"},
			rows:  pageRows(2),
			meta:  truncatedMeta(),
			want: `{"items":[{"uid":"uid-0","name":"name-0"},{"uid":"uid-1","name":"name-1"}],` +
				`"list_meta":{"truncated":true,"returned":2,"total":5,"continue":"gcx x list --limit 0"}}`,
			match: "json",
		},
		{
			name:  "yaml truncated",
			flags: map[string]string{"output": "yaml"},
			rows:  pageRows(1),
			meta:  &cmdio.ListMeta{Truncated: true, Returned: 1, Continue: "gcx x list --limit 2"},
			want: "items:\n- name: name-0\n  uid: uid-0\n" +
				"list_meta:\n  continue: gcx x list --limit 2\n  returned: 1\n  truncated: true\n",
			match: "yaml",
		},
		{
			name:  "json field selection applies to items and keeps list_meta",
			flags: map[string]string{"json": "uid"},
			rows:  pageRows(2),
			meta:  truncatedMeta(),
			want: `{"items":[{"uid":"uid-0"},{"uid":"uid-1"}],` +
				`"list_meta":{"truncated":true,"returned":2,"total":5,"continue":"gcx x list --limit 0"}}`,
			match: "json",
		},
		{
			name:  "json field discovery lists item fields only",
			flags: map[string]string{"json": "list"},
			rows:  pageRows(2),
			meta:  truncatedMeta(),
			want:  "name\nuid\n",
			match: "exact",
		},
		{
			name:  "json field discovery on an empty page lists item fields",
			flags: map[string]string{"json": "list"},
			rows:  nil,
			want:  "name\nuid\n",
			match: "exact",
		},
		{
			name:  "jq reads items",
			flags: map[string]string{"jq": "[.items[].uid]"},
			rows:  pageRows(2),
			meta:  truncatedMeta(),
			want:  `["uid-0","uid-1"]`,
			match: "json",
		},
		{
			name:  "jq reads list_meta",
			flags: map[string]string{"jq": ".list_meta.truncated"},
			rows:  pageRows(2),
			meta:  truncatedMeta(),
			want:  `true`,
			match: "json",
		},
		{
			name:  "table gets the bare items",
			flags: map[string]string{},
			rows:  pageRows(2),
			meta:  truncatedMeta(),
			want:  "UID    NAME\nuid-0  name-0\nuid-1  name-1\n",
			match: "exact",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := encodeListWithFlags(t, tc.flags, tc.rows, tc.meta)
			switch tc.match {
			case "json":
				assert.JSONEq(t, tc.want, got)
			case "yaml":
				assert.YAMLEq(t, tc.want, got)
			default:
				assert.Equal(t, tc.want, got)
			}
			if tc.match == "exact" {
				assert.NotContains(t, got, "list_meta")
			}
		})
	}
}

// TestEncodeListAgentsSpillCountsItems checks that a spilled envelope reports
// the number of items, not the number of envelope keys.
func TestEncodeListAgentsSpillCountsItems(t *testing.T) {
	t.Setenv("GCX_AGENT_SPILL_BYTES", "64")
	t.Setenv("TMPDIR", t.TempDir())

	out := encodeListWithFlags(t, map[string]string{"output": "agents"}, pageRows(10), truncatedMeta())

	var receipt struct {
		Type       string `json:"type"`
		TotalItems *int   `json:"total_items"`
	}
	require.NoError(t, json.NewDecoder(strings.NewReader(out)).Decode(&receipt), out)
	assert.Equal(t, cmdio.SpillReferenceType, receipt.Type)
	require.NotNil(t, receipt.TotalItems)
	assert.Equal(t, 10, *receipt.TotalItems)
}

func TestIsStructuredFormat(t *testing.T) {
	tests := []struct {
		format string
		want   bool
	}{
		{"json", true},
		{"yaml", true},
		{"agents", true},
		{"table", false},
		{"unknown", false},
	}
	for _, tc := range tests {
		t.Run(tc.format, func(t *testing.T) {
			opts := &cmdio.Options{}
			cmdio.RegisterTable(opts, pageRowTable())
			opts.BindFlags(pflag.NewFlagSet("test", pflag.ContinueOnError))
			opts.OutputFormat = tc.format
			assert.Equal(t, tc.want, opts.IsStructuredFormat())
		})
	}
}
