package irm

import (
	"github.com/grafana/gcx/internal/resources"
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
			Factory:    NewIncidentAdapterFactory(loader),
			Descriptor: desc,
			GVK:        desc.GroupVersionKind(),
			Schema:     IncidentSchema,
			Example:    IncidentExample(),
			CheckMutation: func(operation string) error {
				if operation == "delete" {
					return &resources.UnsupportedResourceError{Selector: "incidents", Reason: "delete is not supported by the IRM API"}
				}
				return nil
			},
			URLTemplate: "/a/grafana-irm-app/incidents/{name}",
		},
	}
}
