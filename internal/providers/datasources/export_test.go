package datasources

import (
	dsclient "github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/resources/adapter"
)

// Test-only exports for the unexported manifest mapping helpers.
//
//nolint:gochecknoglobals // Test exports.
var (
	DatasourceToUnstructured = datasourceToUnstructured
	UnstructuredToDatasource = unstructuredToDatasource
)

// NewAdapterForTest builds the resource adapter over a fake transport.
func NewAdapterForTest(client dsclient.Transport, namespace string) adapter.ResourceAdapter {
	return &datasourceAdapter{client: client, namespace: namespace, desc: StaticDescriptor()}
}
