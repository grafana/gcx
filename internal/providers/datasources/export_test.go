package datasources

// Test-only exports for the unexported manifest mapping helpers.
//
//nolint:gochecknoglobals // Test exports.
var (
	DatasourceToUnstructured = datasourceToUnstructured
	UnstructuredToDatasource = unstructuredToDatasource
)
