package checks

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsBase64(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"empty string", "", true},
		{"valid base64", base64.StdEncoding.EncodeToString([]byte("console.log('hi')")), true},
		{"plaintext js", "export default function() { console.log('hi'); }", false},
		{"plaintext with invalid chars", "hello world!", false},
		{"non-canonical padding", "aGVsbG8", false}, // valid chars, but not canonically padded
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isBase64(tt.in))
		})
	}
}

func scriptedSettings(script string) CheckSettings {
	return CheckSettings{"scripted": map[string]any{"script": script}}
}

func browserSettings(script string) CheckSettings {
	return CheckSettings{"browser": map[string]any{"script": script}}
}

// scriptFromSettings extracts settings[checkType]["script"] with checked type
// assertions, failing the test immediately if the shape doesn't match.
func scriptFromSettings(t *testing.T, s CheckSettings, checkType string) string {
	t.Helper()
	nested, ok := s[checkType].(map[string]any)
	require.True(t, ok, "settings[%q] is not a map[string]any", checkType)
	script, ok := nested["script"].(string)
	require.True(t, ok, "settings[%q][\"script\"] is not a string", checkType)
	return script
}

func TestDecodeScriptSettings(t *testing.T) {
	plaintext := "export default function() {}"
	encoded := base64.StdEncoding.EncodeToString([]byte(plaintext))

	t.Run("scripted with base64 script decodes", func(t *testing.T) {
		out, changed := decodeScriptSettings(scriptedSettings(encoded))
		assert.True(t, changed)
		assert.Equal(t, plaintext, scriptFromSettings(t, out, "scripted"))
	})

	t.Run("browser with base64 script decodes", func(t *testing.T) {
		out, changed := decodeScriptSettings(browserSettings(encoded))
		assert.True(t, changed)
		assert.Equal(t, plaintext, scriptFromSettings(t, out, "browser"))
	})

	t.Run("already plaintext is left unchanged", func(t *testing.T) {
		in := scriptedSettings(plaintext)
		out, changed := decodeScriptSettings(in)
		assert.False(t, changed)
		assert.Equal(t, in, out)
	})

	t.Run("non-script check type is untouched", func(t *testing.T) {
		in := CheckSettings{"http": map[string]any{"method": "GET"}}
		out, changed := decodeScriptSettings(in)
		assert.False(t, changed)
		assert.Equal(t, in, out)
	})

	t.Run("missing settings map", func(t *testing.T) {
		out, changed := decodeScriptSettings(nil)
		assert.False(t, changed)
		assert.Nil(t, out)
	})

	t.Run("original map is not mutated", func(t *testing.T) {
		in := scriptedSettings(encoded)
		nestedBefore := scriptFromSettings(t, in, "scripted")
		_, changed := decodeScriptSettings(in)
		assert.True(t, changed)
		assert.Equal(t, nestedBefore, scriptFromSettings(t, in, "scripted"), "input settings must not be mutated")
	})
}

func TestEncodeScriptSettingsIfPlaintext(t *testing.T) {
	plaintext := "export default function() {}"
	encoded := base64.StdEncoding.EncodeToString([]byte(plaintext))

	t.Run("plaintext scripted script gets encoded", func(t *testing.T) {
		out := encodeScriptSettingsIfPlaintext(scriptedSettings(plaintext))
		assert.Equal(t, encoded, scriptFromSettings(t, out, "scripted"))
	})

	t.Run("plaintext browser script gets encoded", func(t *testing.T) {
		out := encodeScriptSettingsIfPlaintext(browserSettings(plaintext))
		assert.Equal(t, encoded, scriptFromSettings(t, out, "browser"))
	})

	t.Run("already base64 script is left unchanged", func(t *testing.T) {
		in := scriptedSettings(encoded)
		out := encodeScriptSettingsIfPlaintext(in)
		assert.Equal(t, in, out)
	})

	t.Run("non-script check type is untouched", func(t *testing.T) {
		in := CheckSettings{"http": map[string]any{"method": "GET"}}
		out := encodeScriptSettingsIfPlaintext(in)
		assert.Equal(t, in, out)
	})

	t.Run("missing settings map", func(t *testing.T) {
		out := encodeScriptSettingsIfPlaintext(nil)
		assert.Nil(t, out)
	})

	t.Run("round trip through decode then encode is stable", func(t *testing.T) {
		decoded, changed := decodeScriptSettings(scriptedSettings(encoded))
		assert.True(t, changed)
		reEncoded := encodeScriptSettingsIfPlaintext(decoded)
		assert.Equal(t, encoded, scriptFromSettings(t, reEncoded, "scripted"))
	})
}
