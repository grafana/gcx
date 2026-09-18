package templates_test

import (
	"encoding/json"
	"testing"

	"github.com/grafana/gcx/internal/providers/agento11y/eval"
	"github.com/grafana/gcx/internal/providers/agento11y/eval/templates"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/require"
)

func TestTableGolden(t *testing.T) {
	var row eval.TemplateDefinition
	require.NoError(t, json.Unmarshal([]byte(`{"template_id": "template-1", "scope": "tenant", "kind": "llm", "latest_version": "v2", "description": "description \u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c", "created_by": "reviewer", "created_at": "2026-09-01T12:30:00Z"}`), &row))
	testutils.AssertTableGolden(t, templates.Table(), row, "table", "wide")
}

func TestVersionsTableGolden(t *testing.T) {
	var row eval.TemplateVersion
	require.NoError(t, json.Unmarshal([]byte(`{"version": "v2", "changelog": "changelog \u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c", "created_by": "reviewer", "created_at": "2026-09-01T12:30:00Z"}`), &row))
	testutils.AssertTableGolden(t, templates.VersionsTable(), row, "table")
}
