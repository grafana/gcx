//nolint:testpackage // white-box testing: accesses unexported codec and helpers.
package explain

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	otelexplain "github.com/grafana/otel-checker/checks/explain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAllEntries_SortedAndNonEmpty(t *testing.T) {
	entries := allEntries()
	require.NotEmpty(t, entries, "explain registry should have at least one entry")

	// Sorted alphabetically by ID.
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

func TestCommand_ShowUnknownID(t *testing.T) {
	cmd := Command()
	cmd.SetArgs([]string{"totally.made-up.id"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown explain ID")
}

func TestCommand_ShowRequiresID(t *testing.T) {
	cmd := Command()
	cmd.SetArgs([]string{})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "an explain ID is required")
}

func TestCommand_ShowKnownID_TextOutput(t *testing.T) {
	ids := otelexplain.All()
	require.NotEmpty(t, ids, "registry is empty — nothing to test against")

	// Pick the first ID; every ID is guaranteed lookupable by the upstream tests.
	id := ids[0]
	doc, ok := otelexplain.Lookup(id)
	require.True(t, ok)

	cmd := Command()
	var buf bytes.Buffer
	cmd.SetArgs([]string{id, "-o", "text"})
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})

	require.NoError(t, cmd.Execute())
	out := buf.String()
	// bytes.Buffer isn't a terminal — codec should emit raw markdown.
	assert.Contains(t, out, "# "+doc.Title)
	assert.Contains(t, out, doc.Body[:min(40, len(doc.Body))])
}

func TestCommand_ShowKnownID_JSONOutput(t *testing.T) {
	ids := otelexplain.All()
	require.NotEmpty(t, ids)
	id := ids[0]
	want, ok := otelexplain.Lookup(id)
	require.True(t, ok)

	cmd := Command()
	var buf bytes.Buffer
	cmd.SetArgs([]string{id, "-o", "json"})
	cmd.SetOut(&buf)
	cmd.SetErr(&bytes.Buffer{})

	require.NoError(t, cmd.Execute())

	var got DocView
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	assert.Equal(t, want.ID, got.ID)
	assert.Equal(t, want.Title, got.Title)
	assert.Equal(t, want.Severity, got.Severity)
	assert.Equal(t, want.Body, got.Body)
}

func TestCommand_List_TableOutput(t *testing.T) {
	cmd := Command()
	var buf bytes.Buffer
	// Force -o table explicitly; the process may be in agent mode
	// (CLAUDECODE env var), which would otherwise default to JSON.
	cmd.SetArgs([]string{"list", "-o", "table"})
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

func TestCommand_List_JSONOutput(t *testing.T) {
	cmd := Command()
	var buf bytes.Buffer
	cmd.SetArgs([]string{"list", "-o", "json"})
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

func TestDocTextCodec_WrongType(t *testing.T) {
	err := (&docTextCodec{}).Encode(&bytes.Buffer{}, "nope")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DocView")
}

func TestEntryTableCodec_WrongType(t *testing.T) {
	err := (&entryTableCodec{}).Encode(&bytes.Buffer{}, "nope")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "EntryListEnvelope")
}

func TestDocTextCodec_NonTerminalOutputsRawMarkdown(t *testing.T) {
	var buf bytes.Buffer
	err := (&docTextCodec{}).Encode(&buf, DocView{
		ID:       "test.id",
		Title:    "Test Title",
		Severity: "error",
		Body:     "The body text.",
	})
	require.NoError(t, err)
	out := buf.String()
	assert.True(t, strings.HasPrefix(out, "# Test Title\n\n"),
		"expected raw markdown header, got: %q", out)
	assert.Contains(t, out, "The body text.")
}
