package templates_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/providers/sigil/eval"
	"github.com/grafana/gcx/internal/providers/sigil/eval/templates"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTableCodec_Encode(t *testing.T) {
	items := []eval.TemplateDefinition{
		{TemplateID: "tpl-1", Scope: "global", Kind: "llm_judge", LatestVersion: "2026-04-01",
			Description: "Quality check", CreatedBy: "admin",
			CreatedAt: time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)},
		{TemplateID: "tpl-2", Scope: "tenant", Kind: "regex", Description: ""},
	}

	tests := []struct {
		name string
		wide bool
		want []string
	}{
		{
			name: "table format",
			wide: false,
			want: []string{"ID", "SCOPE", "KIND", "LATEST VERSION", "tpl-1", "global", "llm_judge", "2026-04-01"},
		},
		{
			name: "wide includes CREATED BY",
			wide: true,
			want: []string{"CREATED BY", "CREATED AT", "admin", "2026-04-01 10:00"},
		},
		{
			name: "empty description shows dash",
			wide: false,
			want: []string{"tpl-2", "-"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			codec := &templates.TableCodec{Wide: tc.wide}
			var buf bytes.Buffer
			require.NoError(t, codec.Encode(&buf, items))

			output := buf.String()
			for _, s := range tc.want {
				assert.Contains(t, output, s)
			}
		})
	}
}

func TestVersionsTableCodec_Encode(t *testing.T) {
	items := []eval.TemplateVersion{
		{Version: "2026-04-01", Changelog: "Initial release", CreatedBy: "admin",
			CreatedAt: time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)},
	}

	codec := &templates.VersionsTableCodec{}
	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, items))

	output := buf.String()
	assert.Contains(t, output, "VERSION")
	assert.Contains(t, output, "CHANGELOG")
	assert.Contains(t, output, "2026-04-01")
	assert.Contains(t, output, "Initial release")
}

func TestTableCodec_WrongType(t *testing.T) {
	codec := &templates.TableCodec{}
	var buf bytes.Buffer
	err := codec.Encode(&buf, "not-a-slice")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected []TemplateDefinition")
}

func TestTableCodec_Format(t *testing.T) {
	tests := []struct {
		wide   bool
		expect string
	}{
		{false, "table"},
		{true, "wide"},
	}
	for _, tc := range tests {
		codec := &templates.TableCodec{Wide: tc.wide}
		assert.Equal(t, tc.expect, string(codec.Format()))
	}
}

func TestTableCodec_DecodeUnsupported(t *testing.T) {
	codec := &templates.TableCodec{}
	err := codec.Decode(nil, nil)
	require.Error(t, err)
}

func TestVersionsTableCodec_WrongType(t *testing.T) {
	codec := &templates.VersionsTableCodec{}
	var buf bytes.Buffer
	err := codec.Encode(&buf, "not-a-slice")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected []TemplateVersion")
}

func TestVersionsTableCodec_DecodeUnsupported(t *testing.T) {
	codec := &templates.VersionsTableCodec{}
	err := codec.Decode(nil, nil)
	require.Error(t, err)
}
