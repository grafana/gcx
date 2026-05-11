package investigations_test

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/internal/assistant/investigations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProfilesTableCodec_Encode(t *testing.T) {
	profiles := []investigations.Profile{
		{ID: "default", Name: "Default", IsDefault: true, Description: "Standard runner"},
		{ID: "arena", Name: "Arena", Description: ""},
	}

	t.Run("table", func(t *testing.T) {
		codec := &investigations.ProfilesTableCodec{}
		assert.Equal(t, "table", string(codec.Format()))

		var buf bytes.Buffer
		require.NoError(t, codec.Encode(&buf, profiles))
		out := buf.String()
		assert.Contains(t, out, "ID")
		assert.Contains(t, out, "NAME")
		assert.Contains(t, out, "DEFAULT")
		assert.NotContains(t, out, "DESCRIPTION")
		assert.Contains(t, out, "default")
		assert.Contains(t, out, "yes")
		assert.Contains(t, out, "no")
	})

	t.Run("wide", func(t *testing.T) {
		codec := &investigations.ProfilesTableCodec{Wide: true}
		assert.Equal(t, "wide", string(codec.Format()))

		var buf bytes.Buffer
		require.NoError(t, codec.Encode(&buf, profiles))
		out := buf.String()
		assert.Contains(t, out, "DESCRIPTION")
		assert.Contains(t, out, "Standard runner")
		assert.Contains(t, out, "-") // empty description
	})

	t.Run("wrong type", func(t *testing.T) {
		codec := &investigations.ProfilesTableCodec{}
		err := codec.Encode(&bytes.Buffer{}, "not profiles")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expected []Profile")
	})

	t.Run("decode unsupported", func(t *testing.T) {
		codec := &investigations.ProfilesTableCodec{}
		require.Error(t, codec.Decode(nil, nil))
	})
}
