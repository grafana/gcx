package loki

// Test helpers — expose internal drilldown encoding functions for the
// external test package.

func EscapePrimaryLabel(value string) string {
	return escapePrimaryLabel(value)
}

func EncodeLabelFilter(key, operator, value string) string {
	return encodeLabelFilter(key, operator, value)
}

func LineFilterKeyAndValue(index int, operator, value string) (string, string) {
	return lineFilterKeyAndValue(index, operator, value)
}
