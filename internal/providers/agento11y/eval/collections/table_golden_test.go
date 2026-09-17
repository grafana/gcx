package collections_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/providers/agento11y/eval/collections"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/require"
)

func TestTableGolden(t *testing.T) {
	var row collections.Collection
	require.NoError(t, json.Unmarshal([]byte(`{"collection_id": "collection-1", "name": "Review cases", "member_count": 4, "description": "description \u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c", "created_by": "reviewer", "created_at": "2026-09-01T12:30:00Z", "updated_at": "2026-09-02T13:45:00Z"}`), &row))
	for _, tc := range []struct {
		name  string
		codec format.Codec
	}{
		{"table", collections.Table().Codec("table")},
		{"wide", collections.Table().Codec("wide")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, rows := range []struct {
				name  string
				items []collections.Collection
			}{
				{"populated", []collections.Collection{row, {}}},
				{"empty", []collections.Collection{}},
				{"nil", nil},
			} {
				t.Run(rows.name, func(t *testing.T) {
					var buf bytes.Buffer
					require.NoError(t, tc.codec.Encode(&buf, rows.items))
					testutils.Golden(t, t.Name(), buf.String())
				})
			}
		})
	}
}
