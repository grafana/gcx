package shared

// ValOrDash returns s if non-empty, otherwise "-".
func ValOrDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// BoolStr returns "true" if b is true, otherwise "false".
func BoolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
