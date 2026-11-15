package observation

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/grafana/gcx/internal/resources"
)

// Supported resource types for capture. Each type has documented
// normalization rules; other types are rejected rather than captured with
// unknown semantics.
const (
	TypeDashboards  = "dashboards.dashboard.grafana.app"
	TypeFolders     = "folders.folder.grafana.app"
	TypeDatasources = "datasources.datasource.grafana.app"
)

// SupportedTypes returns the supported resource type keys in a stable order.
func SupportedTypes() []string {
	return []string{TypeFolders, TypeDashboards, TypeDatasources}
}

// IsSupportedType reports whether a resource type key can be captured.
func IsSupportedType(typeKey string) bool {
	return slices.Contains(SupportedTypes(), typeKey)
}

// supportedTypeNames are the names that select each supported type: plural,
// singular and lowercase kind. A selector group must be empty or a prefix of
// the type's group.
//
//nolint:gochecknoglobals // constant-like lookup table.
var supportedTypeNames = []struct {
	key   string
	group string
	names []string
}{
	{TypeFolders, "folder.grafana.app", []string{"folders", "folder"}},
	{TypeDashboards, "dashboard.grafana.app", []string{"dashboards", "dashboard"}},
	{TypeDatasources, "datasource.grafana.app", []string{"datasources", "datasource"}},
}

// supportedTypeFor returns the supported type key that a selector names, or
// "" when it names no supported type. It does not need the stack's discovery
// data, so it also works for a type that the stack does not serve.
func supportedTypeFor(gvk resources.PartialGVK) string {
	name := strings.ToLower(gvk.Resource)
	group := strings.ToLower(gvk.Group)
	for _, t := range supportedTypeNames {
		if !slices.Contains(t.names, name) {
			continue
		}
		if group == "" || group == t.group || strings.HasPrefix(t.group, group+".") {
			return t.key
		}
	}
	return ""
}

// itemReadTypes are types whose list endpoint returns summaries, so each item
// is read again individually. The legacy datasource list omits fields such as
// basicAuthUser and the names of configured secrets.
//
//nolint:gochecknoglobals // constant-like lookup table.
var itemReadTypes = map[string]bool{
	TypeDatasources: true,
}

// redactedSecret replaces every secure value in a capture. Its presence means
// that a secret with that name is configured; its value is never observed.
//
//nolint:gochecknoglobals // constant-like marker value.
var redactedSecret = map[string]any{"redacted": true}

// incidentalAnnotations are server- or tool-written annotations that change
// on every save without changing observable configuration.
//
//nolint:gochecknoglobals // constant-like lookup table.
var incidentalAnnotations = map[string]bool{
	"grafana.app/createdBy":                            true,
	"grafana.app/updatedBy":                            true,
	"grafana.app/updatedTimestamp":                     true,
	"grafana.app/message":                              true,
	"grafana.app/sourceChecksum":                       true,
	"grafana.app/sourceTimestamp":                      true,
	"kubectl.kubernetes.io/last-applied-configuration": true,
	// The folder path annotations are derived from the folder tree. The
	// parent is kept in grafana.app/folder, so a rename or move is still
	// observed on the folder that changed, not on all of its descendants.
	"grafana.app/fullpath":     true,
	"grafana.app/fullpathUIDs": true,
}

// incidentalLabels are server-written labels without configuration meaning.
//
//nolint:gochecknoglobals // constant-like lookup table.
var incidentalLabels = map[string]bool{
	"grafana.app/deprecatedInternalID": true,
}

// incidentalSpecFields are per-type spec fields that hold revision metadata.
//
//nolint:gochecknoglobals // constant-like lookup table.
var incidentalSpecFields = map[string][]string{
	// The dashboard JSON carries a save counter and a legacy numeric ID.
	TypeDashboards: {"version", "id"},
}

// Normalize returns the observable form of a resource object. Normalization
// version 1 applies these rules:
//
//   - apiVersion, kind and metadata.name are kept.
//   - metadata.labels and metadata.annotations are kept, except incidental
//     server-written keys (createdBy, updatedBy, updatedTimestamp, message,
//     sourceChecksum, sourceTimestamp, kubectl last-applied, deprecated
//     internal ID) and the derived folder path keys (fullpath,
//     fullpathUIDs). Empty maps are removed.
//   - metadata.finalizers and metadata.ownerReferences are kept.
//   - Other metadata (namespace, uid, resourceVersion, generation,
//     creationTimestamp, managedFields, deletion fields) is removed. The
//     namespace is recorded once in the manifest.
//   - spec is kept as read, including array order. For dashboards, the
//     spec.version save counter and the legacy spec.id are removed.
//   - status is removed: it is server-managed state.
//   - Every value under secure is replaced by {"redacted": true}. Only the
//     secret names are observed; a hidden change of a secret value cannot be
//     detected.
//   - Other top-level fields are kept as read.
//
// The input is not modified.
func Normalize(typeKey string, obj map[string]any) (map[string]any, error) {
	clone, err := deepCopy(obj)
	if err != nil {
		return nil, err
	}

	out := map[string]any{}
	for key, value := range clone {
		switch key {
		case "metadata", "status":
			// Handled below or removed.
		case "secure":
			out["secure"] = redactSecure(value)
		default:
			out[key] = value
		}
	}

	meta, _ := clone["metadata"].(map[string]any)
	normMeta := map[string]any{}
	if name, ok := meta["name"]; ok {
		normMeta["name"] = name
	}
	if labels := filterKeys(meta["labels"], incidentalLabels); labels != nil {
		normMeta["labels"] = labels
	}
	if annotations := filterKeys(meta["annotations"], incidentalAnnotations); annotations != nil {
		normMeta["annotations"] = annotations
	}
	for _, key := range []string{"finalizers", "ownerReferences"} {
		if value, ok := meta[key]; ok {
			normMeta[key] = value
		}
	}
	out["metadata"] = normMeta

	if spec, ok := out["spec"].(map[string]any); ok {
		for _, field := range incidentalSpecFields[typeKey] {
			delete(spec, field)
		}
	}

	return out, nil
}

func redactSecure(value any) any {
	secure, ok := value.(map[string]any)
	if !ok {
		// An unexpected shape is replaced as a whole: it must not leak.
		return redactedSecret
	}
	out := make(map[string]any, len(secure))
	for name := range secure {
		out[name] = map[string]any{"redacted": true}
	}
	return out
}

func filterKeys(value any, drop map[string]bool) map[string]any {
	in, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	out := map[string]any{}
	for key, v := range in {
		if !drop[key] {
			out[key] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// deepCopy round-trips through JSON so that numbers and maps have the same
// representation as a capture read back from disk.
func deepCopy(obj map[string]any) (map[string]any, error) {
	data, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("normalize: marshal: %w", err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("normalize: unmarshal: %w", err)
	}
	return out, nil
}
