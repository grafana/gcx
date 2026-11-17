package resources

import (
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// HasServerDerivedName identifies the native kinds whose API names are opaque,
// server-derived addresses. The policy is independent of the served version.
func HasServerDerivedName(gvk schema.GroupVersionKind) bool {
	return gvk.Group == "notifications.alerting.grafana.app" && (gvk.Kind == "TimeInterval" || gvk.Kind == "TemplateGroup")
}

// NativeIdentity validates the logical identity of an opted-in native object.
// Historical template groups without spec.kind match Grafana-managed groups;
// structured reads and create payloads retain the original spec unchanged.
func NativeIdentity(obj *unstructured.Unstructured) (string, bool, error) {
	if !HasServerDerivedName(obj.GroupVersionKind()) {
		return "", false, fmt.Errorf("no native identity policy for %s", obj.GroupVersionKind())
	}
	field := "name"
	if obj.GetKind() == "TemplateGroup" {
		field = "title"
	}
	title, found, err := unstructured.NestedString(obj.Object, "spec", field)
	if err != nil || !found || title == "" {
		return "", false, fmt.Errorf("spec.%s must be a nonempty string", field)
	}
	if obj.GetKind() == "TimeInterval" {
		return title, IsReadOnlyNative(obj), nil
	}
	kind, found, err := unstructured.NestedString(obj.Object, "spec", "kind")
	if err != nil {
		return "", false, errors.New("spec.kind must be grafana or mimir")
	}
	if !found {
		kind = "grafana"
	}
	if kind != "grafana" && kind != "mimir" {
		return "", false, errors.New("spec.kind must be grafana or mimir")
	}
	// A NUL separator avoids collisions between arbitrary titles and kind names.
	return kind + "\x00" + title, IsReadOnlyNative(obj), nil
}

// PrepareNativeCreate copies a manifest and removes source server identity and
// concurrency fields only for the opted-in kinds. Callers own validation and
// write checks. No encoded name is computed.
func PrepareNativeCreate(obj *unstructured.Unstructured) *unstructured.Unstructured {
	copyObj := obj.DeepCopy()
	if !HasServerDerivedName(obj.GroupVersionKind()) {
		return copyObj
	}
	for _, field := range []string{"name", "generateName", "resourceVersion", "uid", "generation", "creationTimestamp", "deletionTimestamp", "deletionGracePeriodSeconds", "managedFields"} {
		unstructured.RemoveNestedField(copyObj.Object, "metadata", field)
	}
	return copyObj
}

// IsReadOnlyNative reports native alerting objects controlled by another source.
// Unknown server kinds remain readable and are not assumed to be read-only.
func IsReadOnlyNative(obj *unstructured.Unstructured) bool {
	if !HasServerDerivedName(obj.GroupVersionKind()) {
		return false
	}
	kind, _, _ := unstructured.NestedString(obj.Object, "spec", "kind")
	if kind == "mimir" {
		return true
	}
	switch obj.GetAnnotations()["grafana.com/provenance"] {
	case "system", "converted_prometheus", "file":
		return true
	default:
		return false
	}
}

// ReadOnlyNativeError explains an object already classified by IsReadOnlyNative.
func ReadOnlyNativeError(obj *unstructured.Unstructured) error {
	field := "name"
	if obj.GetKind() == "TemplateGroup" {
		field = "title"
	}
	title, _, _ := unstructured.NestedString(obj.Object, "spec", field)
	provenance := obj.GetAnnotations()["grafana.com/provenance"]
	// The remaining read-only sources are converted imports or spec.kind mimir.
	guidance := "it was imported from an external Alertmanager configuration; change it there or finish the import in Grafana"
	switch provenance {
	case "system":
		guidance = "it is built into Grafana and cannot be changed"
	case "file":
		guidance = "it is provisioned from a file; change the provisioning file"
	}
	return fmt.Errorf("%s %q is read-only: %s", obj.GetKind(), title, guidance)
}
