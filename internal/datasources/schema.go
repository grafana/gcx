package datasources

import (
	"encoding/json"

	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Descriptor returns the per-plugin resource descriptor for a datasource type.
func Descriptor(pluginType string) resources.Descriptor {
	return resources.Descriptor{
		GroupVersion: schema.GroupVersion{Group: GroupForPluginID(pluginType), Version: datasourceAPIVersion},
		Kind:         datasourceKind,
		Singular:     "datasource",
		Plural:       "datasources",
	}
}

// ConfigSchema returns a generic JSON Schema envelope for a datasource plugin,
// generated from the manifest spec type. It describes the manifest shape (the
// common config fields) but is not yet a per-plugin config schema — plugin
// jsonData/secureJsonData are opaque until the per-plugin schema source lands.
func ConfigSchema(pluginType string) json.RawMessage {
	return adapter.SchemaFromType[DataSourceSpec](Descriptor(pluginType))
}
