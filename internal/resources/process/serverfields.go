package process

import (
	"github.com/grafana/gcx/internal/grafanameta"
	"github.com/grafana/gcx/internal/resources"
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
	delete(annotations, grafanameta.AnnoKeyCreatedBy)
	delete(annotations, grafanameta.AnnoKeyUpdatedBy)
	delete(annotations, grafanameta.AnnoKeyUpdatedTimestamp)

	// Remove manager fields & source properties if the resource is managed by gcx,
	// because these fields are automatically set on push.
	p, ok := r.Raw.GetManagerProperties()
	if ok && p.Kind == resources.ResourceManagerKind {
		delete(annotations, grafanameta.AnnoKeyManagerKind)
		delete(annotations, grafanameta.AnnoKeyManagerIdentity)
		delete(annotations, grafanameta.AnnoKeyManagerSuspended)
		delete(annotations, grafanameta.AnnoKeyManagerAllowsEdits)

		delete(annotations, grafanameta.AnnoKeySourcePath)
		delete(annotations, grafanameta.AnnoKeySourceChecksum)
		delete(annotations, grafanameta.AnnoKeySourceTimestamp)
	}

	// Remove labels set by the server.
	labels := r.Labels()
	delete(labels, grafanameta.LabelKeyDeprecatedInternalID)

	metadata := map[string]any{
		"name":        r.Name(),
		"annotations": annotations,
		"labels":      labels,
	}
	// Preserve the original namespace for inspection.
	// When pushing, the NamespaceOverrider processor will override
	// this with the target context's namespace.
	// Cluster-scoped objects have no namespace, so none is written.
	if ns := r.Namespace(); ns != "" {
		metadata["namespace"] = ns
	}

	return r.SetUnstructured(&unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": r.APIVersion(),
			"kind":       r.Kind(),
			"metadata":   metadata,
			"spec":       spec,
		},
	})
}

// Name returns the name of the processor.
func (m *ServerFieldsStripper) Name() string {
	return "strip-server-fields"
}
