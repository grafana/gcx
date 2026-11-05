package loki

import "testing"

// Test helpers — expose internal drilldown encoding functions for the
// external test package.

func EscapePrimaryLabel(value string) string {
	return escapePrimaryLabel(value)
}

func EncodeLabelFilter(key, operator, value string) (string, bool) {
	return encodeLabelFilter(key, operator, value)
}

func LineFilterKeyAndValue(index int, operator, value string) (string, string) {
	return lineFilterKeyAndValue(index, operator, value)
}

// SetPolicyURLOverride forces the guard on for the duration of the test and
// points its policy fetch at url.
func SetPolicyURLOverride(t testing.TB, url string) {
	t.Helper()
	prev := policyURLOverride
	policyURLOverride = url
	t.Cleanup(func() { policyURLOverride = prev })
}
