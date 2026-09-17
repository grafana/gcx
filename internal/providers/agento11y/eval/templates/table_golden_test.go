package templates_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/providers/agento11y/eval"
	"github.com/grafana/gcx/internal/providers/agento11y/eval/templates"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/require"
)

func TestTableGolden(t *testing.T) {
	var row eval.TemplateDefinition
	require.NoError(t, json.Unmarshal([]byte(`{"template_id": "template-1", "scope": "tenant", "kind": "llm", "latest_version": "v2", "description": "description \u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c", "created_by": "reviewer", "created_at": "2026-09-01T12:30:00Z"}`), &row))
	for _, tc := range []struct {
		name  string
		codec format.Codec
	}{
		{"table", templates.Table().Codec("table")},
		{"wide", templates.Table().Codec("wide")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, rows := range []struct {
				name  string
				items []eval.TemplateDefinition
			}{
				{"populated", []eval.TemplateDefinition{row, {}}},
				{"empty", []eval.TemplateDefinition{}},
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

func TestVersionsTableGolden(t *testing.T) {
	var row eval.TemplateVersion
	require.NoError(t, json.Unmarshal([]byte(`{"version": "v2", "changelog": "changelog \u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c", "created_by": "reviewer", "created_at": "2026-09-01T12:30:00Z"}`), &row))
	for _, tc := range []struct {
		name  string
		codec format.Codec
	}{
		{"table", templates.VersionsTable().Codec("table")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, rows := range []struct {
				name  string
				items []eval.TemplateVersion
			}{
				{"populated", []eval.TemplateVersion{row, {}}},
				{"empty", []eval.TemplateVersion{}},
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
