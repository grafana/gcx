package deeplink

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/output"
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
	return strings.TrimRight(host, "/") + strings.ReplaceAll(tmpl, "{name}", url.PathEscape(name))
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
// Returns an error if the URL does not use http or https scheme or has no host.
//
// In agent mode no browser is launched — an agent harness has no display,
// and a host browser popping up out of band is never what the operator
// wants. The URL is delivered instead as a typed stderr hint (JSONL
// {"class":"hint"} record) and Open reports success: the link reached the
// consumer. This is the single shared guard — command code must not add
// its own agent-mode branches around browser opens.
func Open(rawURL string) error {
	_, err := OpenWithStatus(rawURL)
	return err
}

// OpenWithStatus is Open, additionally reporting whether a browser launch
// was actually attempted — false when the agent-mode guard skipped it.
// Blocking flows that guide a user (OAuth login) use the status to keep
// their manual-fallback instructions accurate instead of assuming a
// browser appeared.
func OpenWithStatus(rawURL string) (bool, error) {
	if err := validateOpenURL(rawURL); err != nil {
		return false, err
	}
	if agent.IsAgentMode() {
		output.EmitHint(os.Stderr, "browser launch skipped in agent mode; open this URL", rawURL)
		return false, nil
	}
	return true, openURL(rawURL)
}

func validateOpenURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fmt.Errorf("refusing to open non-http URL: %s", rawURL)
	}
	return nil
}
