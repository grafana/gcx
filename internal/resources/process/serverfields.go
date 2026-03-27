package process

import (
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/grafana/pkg/apimachinery/utils"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ServerFieldsStripper is a processor that strips server-side fields from resources.
type ServerFieldsStripper struct{}

// Process strips server-side fields from resources.
func (m *ServerFieldsStripper) Process(r *resources.Resource) error {
	if r.IsEmpty() {
		return nil
	}

	spec, err := r.Spec()
	if err != nil {
		return err
	}

	// Remove annotations set by the server.
	annotations := r.Annotations()
	delete(annotations, utils.AnnoKeyCreatedBy)
	delete(annotations, utils.AnnoKeyUpdatedBy)
	delete(annotations, utils.AnnoKeyUpdatedTimestamp)

	// Remove manager fields & source properties if the resource is managed by gcx,
	// because these fields are automatically set on push.
	p, ok := r.Raw.GetManagerProperties()
	if ok && p.Kind == resources.ResourceManagerKind {
		delete(annotations, utils.AnnoKeyManagerKind)
		delete(annotations, utils.AnnoKeyManagerIdentity)
		delete(annotations, utils.AnnoKeyManagerSuspended)
		delete(annotations, utils.AnnoKeyManagerAllowsEdits)

		delete(annotations, utils.AnnoKeySourcePath)
		delete(annotations, utils.AnnoKeySourceChecksum)
		delete(annotations, utils.AnnoKeySourceTimestamp)
	}

	// Remove labels set by the server.
	labels := r.Labels()
	delete(labels, utils.LabelKeyDeprecatedInternalID)

	return r.SetUnstructured(&unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": r.APIVersion(),
			"kind":       r.Kind(),
			"metadata": map[string]any{
				"name": r.Name(),
				// Preserve the original namespace for inspection.
				// When pushing, the NamespaceOverrider processor will override
				// this with the target context's namespace.
				"namespace":   r.Namespace(),
				"annotations": annotations,
				"labels":      labels,
			},
			"spec": spec,
		},
	})
}

// Name returns the name of the processor.
func (m *ServerFieldsStripper) Name() string {
	return "strip-server-fields"
}
