package checks

import (
	"encoding/base64"
	"maps"
)

// scriptCheckTypes are the check types whose settings carry a base64-encoded
// k6/browser script under settings.<type>.script.
//
//nolint:gochecknoglobals // Static lookup table, mirrors knownCheckTypes in validate.go.
var scriptCheckTypes = map[string]bool{
	"scripted": true,
	"browser":  true,
}

// isBase64 reports whether s is valid standard base64. It round-trips the
// decode through a re-encode rather than trusting DecodeString alone,
// because DecodeString accepts some inputs (e.g. missing padding handled
// leniently by other libraries) inconsistently across encodings — the
// round-trip is the actual "is this the API's encoded form" test.
func isBase64(s string) bool {
	decoded, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return false
	}
	return base64.StdEncoding.EncodeToString(decoded) == s
}

// scriptSettings extracts the nested settings map and script string for a
// scripted/browser check, if present. ok is false for any other check type,
// or if the script field is missing/not a string.
func scriptSettings(s CheckSettings) (string, map[string]any, string, bool) {
	checkType := s.CheckType()
	if !scriptCheckTypes[checkType] {
		return checkType, nil, "", false
	}

	nestedRaw, exists := s[checkType]
	if !exists {
		return checkType, nil, "", false
	}
	nested, isMap := nestedRaw.(map[string]any)
	if !isMap {
		return checkType, nil, "", false
	}

	scriptRaw, exists := nested["script"]
	if !exists {
		return checkType, nested, "", false
	}
	script, isString := scriptRaw.(string)
	if !isString {
		return checkType, nested, "", false
	}

	return checkType, nested, script, true
}

// withScript returns a shallow copy of s with settings[checkType]["script"]
// replaced by newScript. The top-level map and the nested check-type map are
// both copied so the original CheckSettings is left untouched.
func withScript(s CheckSettings, checkType string, nested map[string]any, newScript string) CheckSettings {
	nestedCopy := make(map[string]any, len(nested))
	maps.Copy(nestedCopy, nested)
	nestedCopy["script"] = newScript

	out := make(CheckSettings, len(s))
	maps.Copy(out, s)
	out[checkType] = nestedCopy

	return out
}

// decodeScriptSettings returns a copy of s with a base64-encoded
// scripted/browser script decoded to plaintext, and true if a decode
// happened. Non-script check types, missing script fields, and script
// values that aren't valid base64 are returned unchanged with false.
func decodeScriptSettings(s CheckSettings) (CheckSettings, bool) {
	checkType, nested, script, ok := scriptSettings(s)
	if !ok || !isBase64(script) {
		return s, false
	}

	decoded, err := base64.StdEncoding.DecodeString(script)
	if err != nil {
		return s, false
	}

	return withScript(s, checkType, nested, string(decoded)), true
}

// encodeScriptSettingsIfPlaintext returns a copy of s with a plaintext
// scripted/browser script base64-encoded. If the script is already valid
// base64 (or the check type carries no script at all), s is returned
// unchanged — safe to call unconditionally on every read spec.
func encodeScriptSettingsIfPlaintext(s CheckSettings) CheckSettings {
	checkType, nested, script, ok := scriptSettings(s)
	if !ok || isBase64(script) {
		return s
	}

	encoded := base64.StdEncoding.EncodeToString([]byte(script))
	return withScript(s, checkType, nested, encoded)
}
