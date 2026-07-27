package output_test

import (
	"encoding/json"
	"testing"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The mutation result family is a public machine contract: these tests pin
// the wire shape (discriminators, always-present slices, omitempty behavior)
// so accidental struct changes surface as test failures, not consumer breaks.

func TestBatchMutation_WireShape(t *testing.T) {
	b := cmdio.NewBatchMutation("pushed")
	b.Summary = cmdio.MutationSummary{Succeeded: 2, Failed: 0}

	data, err := json.Marshal(b)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(data, &got))

	assert.Equal(t, "gcx.mutation_batch", got["type"])
	assert.Equal(t, "1", got["schema_version"])
	assert.Equal(t, "pushed", got["action"])
	failures, ok := got["failures"].([]any)
	require.True(t, ok, "failures must serialize as [] even when empty, got %T", got["failures"])
	assert.Empty(t, failures)
	assert.NotContains(t, got, "dry_run", "dry_run must be omitted when false")
}

func TestSingleMutation_WireShape(t *testing.T) {
	m := cmdio.NewSingleMutation("deleted", cmdio.MutationTarget{Kind: "datasource", UID: "abc"})
	changed := true
	m.Changed = &changed

	data, err := json.Marshal(m)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(data, &got))

	assert.Equal(t, "gcx.mutation", got["type"])
	assert.Equal(t, "1", got["schema_version"])
	assert.Equal(t, true, got["changed"])
	target, ok := got["target"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "abc", target["uid"])
	assert.NotContains(t, target, "name", "empty target fields must be omitted")
	assert.NotContains(t, got, "error", "empty error must be omitted")
}

func TestArtifactReceipt_WireShape(t *testing.T) {
	r := cmdio.NewArtifactReceipt("pulled", "json")
	r.Dir = "./out"
	r.Files = append(r.Files, cmdio.ArtifactFile{Path: "./out/dashboards/one.json"})
	r.Summary = cmdio.MutationSummary{Succeeded: 1}

	data, err := json.Marshal(r)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(data, &got))

	assert.Equal(t, "gcx.artifact_receipt", got["type"])
	assert.Equal(t, "1", got["schema_version"])
	assert.Equal(t, "json", got["format"])
	failures, ok := got["failures"].([]any)
	require.True(t, ok, "failures must serialize as [] even when empty")
	assert.Empty(t, failures)
	files, ok := got["files"].([]any)
	require.True(t, ok)
	require.Len(t, files, 1)
}
