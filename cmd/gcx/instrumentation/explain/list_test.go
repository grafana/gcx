//nolint:testpackage // white-box testing: accesses unexported entryTableCodec and allEntries.
package explain

import (
	"bytes"
	"encoding/json"
	"testing"

	otelexplain "github.com/grafana/otel-checker/checks/explain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAllEntries_SortedAndNonEmpty(t *testing.T) {
	entries := allEntries()
	require.NotEmpty(t, entries, "explain registry should have at least one entry")

	// Sorted alphabetically by ID (upstream otelexplain.All() guarantees this).
	for i := 1; i < len(entries); i++ {
		assert.Less(t, entries[i-1].ID, entries[i].ID,
			"entries should be sorted by ID (index %d: %q vs %q)", i, entries[i-1].ID, entries[i].ID)
	}

	// Every entry has non-empty required fields.
	for _, e := range entries {
		assert.NotEmpty(t, e.ID, "entry has empty ID")
		assert.NotEmpty(t, e.Title, "entry %q has empty title", e.ID)
		assert.NotEmpty(t, e.Severity, "entry %q has empty severity", e.ID)
	}
}

func TestListCommand_TableOutput(t *testing.T) {
	cmd := ListCommand()
	var buf bytes.Buffer
	// Force -o table explicitly; the process may be in agent mode
	// (CLAUDECODE env var), which would otherwise default to JSON.
	cmd.SetArgs([]string{"-o", "table"})
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})

	require.NoError(t, cmd.Execute())
	out := buf.String()
	assert.Contains(t, out, "ID")
	assert.Contains(t, out, "SEVERITY")
	assert.Contains(t, out, "TITLE")
	// At least one real ID row present.
	ids := otelexplain.All()
	require.NotEmpty(t, ids)
	assert.Contains(t, out, ids[0])
}

func TestListCommand_JSONOutput(t *testing.T) {
	cmd := ListCommand()
	var buf bytes.Buffer
	cmd.SetArgs([]string{"-o", "json"})
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})

	require.NoError(t, cmd.Execute())

	var got EntryListEnvelope
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	require.NotEmpty(t, got.Items)
	// Envelope items match registry.
	ids := otelexplain.All()
	assert.Len(t, got.Items, len(ids))
	assert.Equal(t, ids[0], got.Items[0].ID)
}

func TestListCommand_RejectsPositionalArgs(t *testing.T) {
	cmd := ListCommand()
	cmd.SetArgs([]string{"extra"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.Execute()
	require.Error(t, err)
}

func TestEntryTableCodec_WrongType(t *testing.T) {
	err := (&entryTableCodec{}).Encode(&bytes.Buffer{}, "nope")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "EntryListEnvelope")
}
