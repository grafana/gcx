package query

import "strings"

// Escape tokens the Scenes ad-hoc filter URL handler decodes back to | , and #.
const (
	scenesPipeToken  = "__gfp__"
	scenesCommaToken = "__gfc__"
	scenesHashToken  = "__gfh__"
)

// EscapeScenesFilterField escapes a filter key or value so it survives the Scenes
// URL handler's split on #, |, and ,. ok is false if it already contains an escape token.
func EscapeScenesFilterField(field string) (string, bool) {
	if strings.Contains(field, scenesPipeToken) || strings.Contains(field, scenesCommaToken) || strings.Contains(field, scenesHashToken) {
		return "", false
	}
	field = strings.ReplaceAll(field, "|", scenesPipeToken)
	field = strings.ReplaceAll(field, ",", scenesCommaToken)
	field = strings.ReplaceAll(field, "#", scenesHashToken)
	return field, true
}
