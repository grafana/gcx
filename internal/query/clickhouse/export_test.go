package clickhouse

// Test helpers — expose internal functions for external test package.

// ParseResponse exposes parseResponse for testing.
func ParseResponse(body []byte) (*QueryResponse, error) {
	return parseResponse(body)
}
