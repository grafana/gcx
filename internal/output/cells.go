package output

// OrDash renders an empty cell as the "-" placeholder tables use for an
// absent value.
func OrDash(s string) string {
	if s == "" {
		return "-"
	}

	return s
}
