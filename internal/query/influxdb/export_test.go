package influxdb

// ConvertGrafanaResponse exposes convertGrafanaResponse for external tests.
func ConvertGrafanaResponse(resp *GrafanaQueryResponse) *QueryResponse {
	return convertGrafanaResponse(resp)
}
