package process

import (
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/version"
	"github.com/grafana/grafana/pkg/apimachinery/utils"
)

// ManagerIdentity returns the manager identity that gcx writes on a pushed resource.
// The value carries the gcx version, so an operator can see which build pushed the
// resource. A build without version information reports "gcx/SNAPSHOT".
func ManagerIdentity() string {
	return "gcx/" + version.Get()
}

// ManagerFieldsAppender is a processor that appends manager and source fields to a resource.
// It will return an error if the resource is already managed by another manager.
type ManagerFieldsAppender struct {
}

func (m *ManagerFieldsAppender) Process(r *resources.Resource) error {
	if r.IsEmpty() {
		return nil
	}

	if !r.IsManaged() {
		// If the resource is not managed by gcx,
		// we don't want to set the manager fields.
		return nil
	}

	r.Raw.SetManagerProperties(utils.ManagerProperties{
		Kind:        resources.ResourceManagerKind,
		Identity:    ManagerIdentity(),
		AllowsEdits: true,
	})

	// The checksum and the timestamp stay empty. Grafana uses both fields to
	// reconcile a resource from a source over time. gcx pushes one time per
	// command, and no gcx code reads the two fields back.
	r.Raw.SetSourceProperties(utils.SourceProperties{
		Path: r.Source.String(),
	})

	return nil
}
