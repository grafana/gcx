package influxdb

// ConvertGrafanaResponse exposes convertGrafanaResponse for external tests.
func ConvertGrafanaResponse(resp *GrafanaQueryResponse) *QueryResponse {
	return convertGrafanaResponse(resp)
}

// ExtractFieldKeys exposes extractFieldKeys for external tests.
func ExtractFieldKeys(resp *GrafanaQueryResponse) *FieldKeysResponse {
	return extractFieldKeys(resp)
}
