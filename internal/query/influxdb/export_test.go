package influxdb

// ConvertGrafanaResponse exposes convertGrafanaResponse for external tests.
func ConvertGrafanaResponse(resp *GrafanaQueryResponse) *QueryResponse {
	return convertGrafanaResponse(resp)
}

// ExtractFieldKeys exposes extractFieldKeys for external tests.
func ExtractFieldKeys(resp *GrafanaQueryResponse) *FieldKeysResponse {
	return extractFieldKeys(resp)
}

// ExtractTagKeys exposes extractTagKeys for external tests.
func ExtractTagKeys(resp *GrafanaQueryResponse) *TagKeysResponse {
	return extractTagKeys(resp)
}

// ExtractTagValues exposes extractTagValues for external tests.
func ExtractTagValues(resp *GrafanaQueryResponse) *TagValuesResponse {
	return extractTagValues(resp)
}
