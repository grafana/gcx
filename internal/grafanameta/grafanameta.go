// Package grafanameta reads and writes the Grafana-specific metadata of
// Grafana API objects: the grafana.app/* annotations and labels that record
// who manages a resource and where it came from.
//
// It covers what gcx needs from Grafana's pkg/apimachinery/utils, for the
// unstructured objects gcx works with, without depending on that module.
package grafanameta

import (
	"errors"
	"strconv"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Annotation and label keys Grafana sets on its API objects.
const (
	AnnoKeyCreatedBy          = "grafana.app/createdBy"
	AnnoKeyUpdatedBy          = "grafana.app/updatedBy"
	AnnoKeyUpdatedTimestamp   = "grafana.app/updatedTimestamp"
	AnnoKeyFolder             = "grafana.app/folder"
	AnnoKeyManagerKind        = "grafana.app/managedBy"
	AnnoKeyManagerIdentity    = "grafana.app/managerId"
	AnnoKeyManagerAllowsEdits = "grafana.app/managerAllowsEdits"
	AnnoKeyManagerSuspended   = "grafana.app/managerSuspended"
	AnnoKeySourcePath         = "grafana.app/sourcePath"
	AnnoKeySourceChecksum     = "grafana.app/sourceChecksum"
	AnnoKeySourceTimestamp    = "grafana.app/sourceTimestamp"

	LabelKeyDeprecatedInternalID = "grafana.app/deprecatedInternalID"

	// annoKeyRepoName is the manager identity's older key, which implies a
	// repo manager. Grafana still reads it, and drops it when the manager is
	// set.
	annoKeyRepoName = "grafana.app/repoName"
)

// ManagerKind is the kind of tool that manages a resource.
type ManagerKind string

// Manager kinds Grafana knows. Any other value reads as ManagerKindUnknown.
const (
	ManagerKindUnknown   ManagerKind = ""
	ManagerKindRepo      ManagerKind = "repo"
	ManagerKindTerraform ManagerKind = "terraform"
	ManagerKindKubectl   ManagerKind = "kubectl"
	ManagerKindPlugin    ManagerKind = "plugin"
	// ManagerKindClassicFP marks resources from legacy file provisioning.
	ManagerKindClassicFP ManagerKind = "classic-file-provisioning"
)

func parseManagerKind(v string) ManagerKind {
	switch k := ManagerKind(v); k {
	case ManagerKindRepo, ManagerKindTerraform, ManagerKindKubectl, ManagerKindPlugin, ManagerKindClassicFP:
		return k
	default:
		return ManagerKindUnknown
	}
}

// ManagerProperties says which tool manages a resource.
type ManagerProperties struct {
	Kind        ManagerKind
	Identity    string
	AllowsEdits bool
	Suspended   bool
}

// SourceProperties says where a managed resource came from.
type SourceProperties struct {
	Path            string
	Checksum        string
	TimestampMillis int64
}

// Accessor reads and writes Grafana metadata on an unstructured object. It
// embeds the object, so the standard metadata getters and setters are
// available too, and changes go to the object itself.
type Accessor struct {
	*unstructured.Unstructured
}

// MetaAccessor returns an Accessor for obj.
func MetaAccessor(obj *unstructured.Unstructured) (*Accessor, error) {
	if obj == nil {
		return nil, errors.New("unable to read metadata from nil object")
	}
	return &Accessor{obj}, nil
}

// GetGroupVersionKind returns the object's group, version and kind.
func (a *Accessor) GetGroupVersionKind() schema.GroupVersionKind {
	return a.GroupVersionKind()
}

// GetSpec returns the object's spec, or an error if it has none.
func (a *Accessor) GetSpec() (any, error) {
	spec, ok := a.Object["spec"]
	if !ok {
		return nil, errors.New("unable to read spec")
	}
	return spec, nil
}

// GetStatus returns the object's status, or an error if it has none.
func (a *Accessor) GetStatus() (any, error) {
	status, ok := a.Object["status"]
	if !ok {
		return nil, errors.New("unable to read status")
	}
	return status, nil
}

// FindTitle returns spec.title, else spec.name, else defaultTitle.
func (a *Accessor) FindTitle(defaultTitle string) string {
	for _, field := range []string{"title", "name"} {
		if v, ok, _ := unstructured.NestedString(a.Object, "spec", field); ok && v != "" {
			return v
		}
	}
	return defaultTitle
}

// GetFolder returns the UID of the folder the object is in, or "" for none.
func (a *Accessor) GetFolder() string {
	return a.GetAnnotations()[AnnoKeyFolder]
}

// GetManagerProperties returns the object's manager, and false if it has
// none.
func (a *Accessor) GetManagerProperties() (ManagerProperties, bool) {
	annot := a.GetAnnotations()
	id := annot[AnnoKeyManagerIdentity]
	if id == "" {
		if repo := annot[annoKeyRepoName]; repo != "" {
			return ManagerProperties{Kind: ManagerKindRepo, Identity: repo}, true
		}
		return ManagerProperties{}, false
	}
	return ManagerProperties{
		Kind:        parseManagerKind(annot[AnnoKeyManagerKind]),
		Identity:    id,
		AllowsEdits: annot[AnnoKeyManagerAllowsEdits] == "true",
		Suspended:   annot[AnnoKeyManagerSuspended] == "true",
	}, true
}

// SetManagerProperties records the object's manager. Zero fields remove
// their annotations.
func (a *Accessor) SetManagerProperties(v ManagerProperties) {
	annot := a.GetAnnotations()
	if annot == nil {
		annot = make(map[string]string, 4)
	}
	set(annot, AnnoKeyManagerIdentity, v.Identity)
	set(annot, AnnoKeyManagerKind, string(v.Kind))
	set(annot, AnnoKeyManagerAllowsEdits, formatTrue(v.AllowsEdits))
	set(annot, AnnoKeyManagerSuspended, formatTrue(v.Suspended))
	delete(annot, annoKeyRepoName)
	a.SetAnnotations(annot)
}

// SetSourceProperties records where the object came from. Zero fields
// remove their annotations.
func (a *Accessor) SetSourceProperties(v SourceProperties) {
	annot := a.GetAnnotations()
	if annot == nil {
		annot = make(map[string]string, 3)
	}
	set(annot, AnnoKeySourcePath, v.Path)
	set(annot, AnnoKeySourceChecksum, v.Checksum)
	ts := ""
	if v.TimestampMillis > 0 {
		ts = strconv.FormatInt(v.TimestampMillis, 10)
	}
	set(annot, AnnoKeySourceTimestamp, ts)
	a.SetAnnotations(annot)
}

func set(annot map[string]string, key, value string) {
	if value == "" {
		delete(annot, key)
	} else {
		annot[key] = value
	}
}

func formatTrue(b bool) string {
	if b {
		return "true"
	}
	return ""
}
