package telemetry

// CapturedDatasourcePluginType is the datasource plugin type recorded by a
// query command's RunE, read once at exit by buildUsageEvent. Empty when no
// query command ran.
//
//nolint:gochecknoglobals
var CapturedDatasourcePluginType string

// RecordDatasourceQueryType records the datasource plugin type for the
// usage event. The raw plugin ID is stored as-is.
func RecordDatasourceQueryType(rawPluginID string) {
	CapturedDatasourcePluginType = rawPluginID
}
