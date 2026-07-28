//nolint:testpackage // white-box testing: accesses unexported docTextCodec.
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

func TestDocTextCodec_WrongType(t *testing.T) {
	err := (&docTextCodec{}).Encode(&bytes.Buffer{}, "nope")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DocView")
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
