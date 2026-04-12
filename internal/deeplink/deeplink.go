package deeplink

import (
	"strings"
	"sync"

	"github.com/pkg/browser"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// groupKind is the version-agnostic lookup key for URL patterns.
// API versions vary by Grafana server version (e.g., v0alpha1, v1alpha1, v1beta1),
// but the deep link URL pattern is the same regardless of version.
type groupKind struct {
	Group string
	Kind  string
}

// registry maps group+kind → URL path template (e.g., "/d/{name}").
//
//nolint:gochecknoglobals // Self-registration pattern (same as providers.registry).
var (
	mu       sync.RWMutex
	patterns = map[groupKind]string{}
)

// InvestigationGVK returns the synthetic GVK for investigations (not adapter-backed).
func InvestigationGVK() schema.GroupVersionKind {
	return schema.GroupVersionKind{Group: "assistant.grafana.app", Version: "v1", Kind: "Investigation"}
}

func init() { //nolint:gochecknoinits // Register K8s-native and non-adapter resource URL patterns.
	// Dashboards and folders are served by Grafana core, not a provider plugin.
	RegisterPattern(schema.GroupVersionKind{Group: "dashboard.grafana.app", Kind: "Dashboard"}, "/d/{name}")
	RegisterPattern(schema.GroupVersionKind{Group: "folder.grafana.app", Kind: "Folder"}, "/dashboards/f/{name}")

	// Investigations are not adapter-backed but have a browser UI.
	RegisterPattern(InvestigationGVK(), "/a/grafana-assistant-app/investigations/{name}")
}

// RegisterPattern associates a URL path template with a GVK.
// The template uses {name} as a placeholder for the resource name.
// The version component of the GVK is ignored — patterns match on group+kind only.
func RegisterPattern(gvk schema.GroupVersionKind, template string) {
	mu.Lock()
	defer mu.Unlock()
	patterns[groupKind{Group: gvk.Group, Kind: gvk.Kind}] = template
}

// Resolve builds a full URL for the given GVK and resource name.
// Returns "" if no pattern is registered for the GVK's group+kind.
func Resolve(host string, gvk schema.GroupVersionKind, name string) string {
	mu.RLock()
	tmpl, ok := patterns[groupKind{Group: gvk.Group, Kind: gvk.Kind}]
	mu.RUnlock()
	if !ok {
		return ""
	}
	return strings.TrimRight(host, "/") + strings.ReplaceAll(tmpl, "{name}", name)
}

// InjectURL sets the top-level "url" field on an unstructured object
// by looking up the group+kind and name from the object itself.
// No-op if no pattern is registered for the object's group+kind.
func InjectURL(obj *unstructured.Unstructured, host string) {
	url := Resolve(host, obj.GroupVersionKind(), obj.GetName())
	if url != "" {
		obj.Object["url"] = url
	}
}

// InjectURLs sets the "url" field on each unstructured object in the slice.
func InjectURLs(items []unstructured.Unstructured, host string) {
	for i := range items {
		InjectURL(&items[i], host)
	}
}

// Open opens the given URL in the default browser.
func Open(url string) error {
	return browser.OpenURL(url)
}
