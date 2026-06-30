package integrations_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/assistant/integrations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListTableCodec_Encode(t *testing.T) {
	items := []integrations.Integration{
		{
			ID:           "int-1",
			Name:         "my-mcp-server",
			Type:         "mcp",
			Scope:        "user",
			Enabled:      boolPtr(true), //nolint:modernize
			Description:  "A test MCP server",
			Applications: []string{"assistant"},
			CreatedBy:    "admin",
			ModifiedAt:   time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC),
		},
		{
			ID:      "int-2",
			Name:    "disabled-server",
			Type:    "mcp",
			Scope:   "tenant",
			Enabled: boolPtr(false), //nolint:modernize
		},
	}

	t.Run("table", func(t *testing.T) {
		codec := &integrations.ListTableCodec{}
		assert.Equal(t, "table", string(codec.Format()))

		var buf bytes.Buffer
		require.NoError(t, codec.Encode(&buf, items))
		out := buf.String()
		assert.Contains(t, out, "NAME")
		assert.Contains(t, out, "ID")
		assert.Contains(t, out, "TYPE")
		assert.Contains(t, out, "SCOPE")
		assert.Contains(t, out, "ENABLED")
		assert.Contains(t, out, "MODIFIED")
		assert.NotContains(t, out, "DESCRIPTION")
		assert.Contains(t, out, "my-mcp-server")
		assert.Contains(t, out, "Yes")
		assert.Contains(t, out, "No")
	})

	t.Run("wide", func(t *testing.T) {
		codec := &integrations.ListTableCodec{Wide: true}
		assert.Equal(t, "wide", string(codec.Format()))

		var buf bytes.Buffer
		require.NoError(t, codec.Encode(&buf, items))
		out := buf.String()
		assert.Contains(t, out, "DESCRIPTION")
		assert.Contains(t, out, "APPLICATIONS")
		assert.Contains(t, out, "CREATED BY")
		assert.Contains(t, out, "A test MCP server")
		assert.Contains(t, out, "assistant")
		assert.Contains(t, out, "admin")
	})

	t.Run("wrong type", func(t *testing.T) {
		codec := &integrations.ListTableCodec{}
		err := codec.Encode(&bytes.Buffer{}, "wrong")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expected []Integration")
	})

	t.Run("decode unsupported", func(t *testing.T) {
		codec := &integrations.ListTableCodec{}
		require.Error(t, codec.Decode(nil, nil))
	})
}

func TestListTableCodec_NameTruncation(t *testing.T) {
	items := []integrations.Integration{
		{
			ID:      "int-1",
			Name:    "this-is-a-very-long-integration-name-that-should-be-truncated",
			Type:    "mcp",
			Scope:   "user",
			Enabled: boolPtr(true), //nolint:modernize
		},
	}

	var buf bytes.Buffer
	codec := &integrations.ListTableCodec{}
	require.NoError(t, codec.Encode(&buf, items))
	assert.Contains(t, buf.String(), "...")
}

func TestListTableCodec_NilEnabled(t *testing.T) {
	items := []integrations.Integration{
		{
			ID:    "int-1",
			Name:  "test",
			Type:  "mcp",
			Scope: "user",
		},
	}

	var buf bytes.Buffer
	codec := &integrations.ListTableCodec{}
	require.NoError(t, codec.Encode(&buf, items))
	// nil enabled displays as "-"
	assert.Contains(t, buf.String(), "-")
}

func TestValidateTableCodec_Encode(t *testing.T) {
	t.Run("success with tools", func(t *testing.T) {
		result := &integrations.ValidationResult{
			Status:  "success",
			Message: "Connected successfully",
			Tools: []integrations.MCPTool{
				{Name: "search", Description: "Search for items"},
				{Name: "create", Description: "Create new items"},
			},
		}

		codec := &integrations.ValidateTableCodec{}
		assert.Equal(t, "table", string(codec.Format()))

		var buf bytes.Buffer
		require.NoError(t, codec.Encode(&buf, result))
		out := buf.String()
		assert.Contains(t, out, "STATUS")
		assert.Contains(t, out, "MESSAGE")
		assert.Contains(t, out, "success")
		assert.Contains(t, out, "Connected successfully")
		assert.Contains(t, out, "TOOL")
		assert.Contains(t, out, "DESCRIPTION")
		assert.Contains(t, out, "search")
		assert.Contains(t, out, "create")
	})

	t.Run("failed without tools", func(t *testing.T) {
		result := &integrations.ValidationResult{
			Status: "failed",
			Error:  "connection refused",
		}

		var buf bytes.Buffer
		codec := &integrations.ValidateTableCodec{}
		require.NoError(t, codec.Encode(&buf, result))
		out := buf.String()
		assert.Contains(t, out, "failed")
		assert.Contains(t, out, "connection refused")
		assert.NotContains(t, out, "TOOL")
	})

	t.Run("wrong type", func(t *testing.T) {
		codec := &integrations.ValidateTableCodec{}
		err := codec.Encode(&bytes.Buffer{}, "wrong")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expected *ValidationResult")
	})

	t.Run("decode unsupported", func(t *testing.T) {
		codec := &integrations.ValidateTableCodec{}
		require.Error(t, codec.Decode(nil, nil))
	})
}
