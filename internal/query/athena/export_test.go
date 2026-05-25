package athena

// ParseResponse exposes parseResponse for testing.
func ParseResponse(body []byte) (*QueryResponse, error) {
	return parseResponse(body)
}
