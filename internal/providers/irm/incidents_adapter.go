package irm

import (
	"github.com/grafana/gcx/internal/resources/adapter"
)

func init() { //nolint:gochecknoinits // Natural key registration for incidents.
	adapter.RegisterNaturalKey(
		incidentStaticDescriptor.GroupVersionKind(),
		adapter.SpecFieldKey("title"),
	)
}

func buildIncidentRegistrations(loader *configLoader) []adapter.Registration {
	desc := incidentStaticDescriptor
	return []adapter.Registration{
		{
			Factory:     NewIncidentAdapterFactory(loader),
			Descriptor:  desc,
			GVK:         desc.GroupVersionKind(),
			Schema:      IncidentSchema(),
			Example:     IncidentExample(),
			URLTemplate: "/a/grafana-incident-app/incidents/{name}",
		},
	}
}
