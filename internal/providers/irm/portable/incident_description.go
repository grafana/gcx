package portable

import (
	"github.com/grafana/gcx/internal/resources"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// IncidentDescriptionStripper omits a field that the Incident API cannot
// write. It runs only when gcx saves portable manifests with resources pull.
type IncidentDescriptionStripper struct{}

func (IncidentDescriptionStripper) Process(res *resources.Resource) error {
	// Keep this package free of IRM registration side effects. An external
	// IRM test checks these literals against the provider GVK constants.
	if res.APIVersion() == "incident.ext.grafana.app/v1alpha1" && res.Kind() == "Incident" {
		unstructured.RemoveNestedField(res.Object.Object, "spec", "description")
	}
	return nil
}
