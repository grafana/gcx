package irm

import (
	"github.com/grafana/gcx/internal/providers/incidents"
	"github.com/grafana/gcx/internal/resources/adapter"
)

func init() { //nolint:gochecknoinits // Natural key registration for incidents.
	adapter.RegisterNaturalKey(
		incidents.StaticDescriptor.GroupVersionKind(),
		adapter.SpecFieldKey("title"),
	)
}

func buildIncidentRegistrations(loader *configLoader) []adapter.Registration {
	desc := incidents.StaticDescriptor
	return []adapter.Registration{
		{
			Factory:     incidents.NewAdapterFactory(loader),
			Descriptor:  desc,
			GVK:         desc.GroupVersionKind(),
			Schema:      incidents.IncidentSchema(),
			Example:     incidents.IncidentExample(),
			URLTemplate: "/a/grafana-incident-app/incidents/{name}",
		},
	}
}
