package discovery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	diskcached "k8s.io/client-go/discovery/cached/disk"
)

// ignoredResourceGroups is a list of resource groups that are supported by Grafana API.
// But are not supposed to be used by the clients just yet.
// (or in case of some groups they are read-only by design)
//
//nolint:gochecknoglobals
var ignoredResourceGroups = []string{
	"apiregistration.k8s.io",
	"featuretoggle.grafana.app",
	"service.grafana.app",
	"userstorage.grafana.app",
	// TODO: check with alerting folks if this should be ignored or not
	"notifications.alerting.grafana.app",
	"iam.grafana.app",
}

// Client is a client that can be used to discover resources.
type Client interface {
	ServerGroupsAndResources() ([]*metav1.APIGroup, []*metav1.APIResourceList, error)
}

// Registry is a registry of resources and their preferred versions.
type Registry struct {
	client   Client
	index    RegistryIndex
	adapters map[schema.GroupVersionKind]adapter.Factory
}

// defaultDiscoveryCacheTTL is how long cached discovery results remain valid.
const defaultDiscoveryCacheTTL = 10 * time.Minute

// NewDefaultRegistry creates a new discovery registry using a disk-cached discovery client.
// It automatically registers all provider adapters into the registry via adapter.RegisterAll,
// so that provider resource types are available alongside native K8s-style resources.
// Discovery results are cached under ~/.cache/gcx/discovery/ (overridable via GCX_DISCOVERY_CACHE_DIR)
// with a 10-minute TTL to avoid redundant API round-trips across CLI invocations.
func NewDefaultRegistry(ctx context.Context, cfg config.NamespacedRESTConfig) (*Registry, error) {
	cacheDir := DiscoveryCacheDir(cfg.Host, "")
	return NewDefaultRegistryWithCacheDir(ctx, cfg, cacheDir)
}

// NewDefaultRegistryWithCacheDir creates a new discovery registry using a disk-cached
// discovery client with the specified cache directory.
func NewDefaultRegistryWithCacheDir(ctx context.Context, cfg config.NamespacedRESTConfig, cacheDir string) (*Registry, error) {
	client, err := diskcached.NewCachedDiscoveryClientForConfig(&cfg.Config, cacheDir, "", defaultDiscoveryCacheTTL)
	if err != nil {
		return nil, err
	}

	reg, err := NewRegistry(ctx, client)
	if err != nil {
		return reg, err
	}

	adapter.RegisterAll(ctx, reg)

	return reg, nil
}

// DiscoveryCacheDir returns the directory to use for caching discovery results.
// Precedence: GCX_DISCOVERY_CACHE_DIR env var > overrideDir > computed default.
// The computed default is a per-server subdirectory under ~/.cache/gcx/discovery/
// using a SHA-256 hash of the server URL. Relative paths in GCX_DISCOVERY_CACHE_DIR
// are ignored to prevent cache writes to unexpected locations.
func DiscoveryCacheDir(serverURL, overrideDir string) string {
	if dir := os.Getenv("GCX_DISCOVERY_CACHE_DIR"); dir != "" && filepath.IsAbs(dir) {
		return dir
	}
	if overrideDir != "" {
		return overrideDir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "gcx", "discovery")
	}
	h := sha256.Sum256([]byte(serverURL))
	return filepath.Join(home, ".cache", "gcx", "discovery", hex.EncodeToString(h[:16]))
}

// NewCachedRegistry creates a new discovery registry from a Client interface.
// This is a test helper — production code uses NewDefaultRegistry which wraps the
// client with disk caching at the HTTP transport level.
func NewCachedRegistry(ctx context.Context, client Client) (*Registry, error) {
	return NewRegistry(ctx, client)
}

// NewRegistry creates a new discovery registry.
//
// The registry will be populated with the resources and their preferred versions
// by calling the server's preferred resources endpoint.
//
// The registry will perform the discovery upon initialization.
func NewRegistry(ctx context.Context, client Client) (*Registry, error) {
	reg := &Registry{
		client:   client,
		index:    NewRegistryIndex(),
		adapters: make(map[schema.GroupVersionKind]adapter.Factory),
	}

	// Perform initial discovery.
	if err := reg.Discover(ctx); err != nil {
		return reg, err
	}

	return reg, nil
}

// NewStaticRegistry creates a registry without a discovery client.
// It only supports statically registered provider descriptors.
// Useful in tests and for scenarios where no Grafana server is available.
func NewStaticRegistry() *Registry {
	return &Registry{
		index:    NewRegistryIndex(),
		adapters: make(map[schema.GroupVersionKind]adapter.Factory),
	}
}

// MakeFiltersOptions contains options for creating filters from selectors.
type MakeFiltersOptions struct {
	// Which selectors to create filters from.
	Selectors resources.Selectors

	// Whether to only return the preferred version of the resource.
	PreferredVersionOnly bool
}

// MakeFilters creates filters from selectors with the given options.
func (r *Registry) MakeFilters(opts MakeFiltersOptions) (resources.Filters, error) {
	var filters resources.Filters

	for _, selector := range opts.Selectors {
		selectorFilters, err := r.makeFiltersForSelector(selector, opts.PreferredVersionOnly)
		if err != nil {
			return nil, err
		}

		filters = append(filters, selectorFilters...)
	}

	return filters, nil
}

// PreferredResources returns all resources with their preferred versions.
func (r *Registry) PreferredResources() resources.Descriptors {
	return r.index.GetPreferredVersions()
}

// SupportedResources returns all resources supported by the server.
func (r *Registry) SupportedResources() resources.Descriptors {
	return r.index.GetDescriptors()
}

// RegisterAdapter registers a provider adapter factory and its static descriptor
// in the registry. The descriptor becomes resolvable through LookupPartialGVK
// alongside dynamically discovered resources.
func (r *Registry) RegisterAdapter(factory adapter.Factory, desc resources.Descriptor, aliases []string) {
	r.index.RegisterStatic(desc, aliases)
	r.adapters[desc.GroupVersionKind()] = factory
}

// GetAdapter returns the adapter factory for the given GVK, if one is registered.
func (r *Registry) GetAdapter(gvk schema.GroupVersionKind) (adapter.Factory, bool) {
	f, ok := r.adapters[gvk]
	return f, ok
}

// HasAdapter reports whether the given GVK is backed by a provider adapter
// rather than the K8s dynamic client.
func (r *Registry) HasAdapter(gvk schema.GroupVersionKind) bool {
	_, ok := r.adapters[gvk]
	return ok
}

// Discover discovers the resources and their preferred versions from the server,
// and stores them in the registry.
func (r *Registry) Discover(ctx context.Context) error {
	apiGroups, apiResources, err := r.client.ServerGroupsAndResources()
	if err != nil {
		return err
	}

	// Filter out ignored resource groups.
	apiGroups, apiResources, err = FilterDiscoveryResults(ignoredResourceGroups, apiGroups, apiResources)
	if err != nil {
		return err
	}

	return r.index.Update(ctx, apiGroups, apiResources)
}

func (r *Registry) makeFiltersForSelector(selector resources.Selector, preferredVersionOnly bool) (resources.Filters, error) {
	// Check if a specific version is provided
	if selector.GroupVersionKind.Version != "" {
		// Version is specified, use single descriptor lookup
		desc, ok := r.index.LookupPartialGVK(selector.GroupVersionKind)
		if !ok {
			return nil, resources.InvalidSelectorError{
				Command: selector.String(),
				Err:     "the server does not support this resource",
			}
		}

		return resources.Filters{{
			Type:         selector.Type,
			ResourceUIDs: selector.ResourceUIDs,
			Descriptor:   desc,
		}}, nil
	}

	// No version specified and preferred version only is requested
	if preferredVersionOnly {
		desc, ok := r.index.LookupPartialGVK(selector.GroupVersionKind)
		if !ok {
			return nil, resources.InvalidSelectorError{
				Command: selector.String(),
				Err:     "the server does not support this resource",
			}
		}

		return resources.Filters{{
			Type:         selector.Type,
			ResourceUIDs: selector.ResourceUIDs,
			Descriptor:   desc,
		}}, nil
	}

	// No version specified and we want all supported versions
	descs, ok := r.index.LookupAllVersionsForPartialGVK(selector.GroupVersionKind)
	if !ok {
		return nil, resources.InvalidSelectorError{
			Command: selector.String(),
			Err:     "the server does not support this resource",
		}
	}

	// Create a filter for each supported version
	filters := make(resources.Filters, 0, len(descs))
	for _, desc := range descs {
		filters = append(filters, resources.Filter{
			Type:         selector.Type,
			ResourceUIDs: selector.ResourceUIDs,
			Descriptor:   desc,
		})
	}

	return filters, nil
}

// FilterDiscoveryResults filters the discovery results to exclude ignored resource groups.
func FilterDiscoveryResults(
	ignored []string, apiGroups []*metav1.APIGroup, apiResources []*metav1.APIResourceList,
) ([]*metav1.APIGroup, []*metav1.APIResourceList, error) {
	filteredGroups := make([]*metav1.APIGroup, 0, len(apiGroups))
	filteredResources := make([]*metav1.APIResourceList, 0, len(apiResources))

	for _, group := range apiGroups {
		if slices.Contains(ignored, group.Name) {
			continue
		}

		filteredGroups = append(filteredGroups, group)
	}

	for _, resource := range apiResources {
		gv, err := parseGroupVersion(resource.GroupVersion)
		if err != nil {
			return nil, nil, err
		}

		if slices.Contains(ignored, gv.Group) {
			continue
		}

		filteredAPIResources := make([]metav1.APIResource, 0, len(resource.APIResources))
		for _, r := range resource.APIResources {
			if !r.Namespaced {
				continue
			}

			// TODO (@radiohead): this excludes subresources, but we should check if that's what we want.
			if strings.Contains(r.Name, "/") {
				continue
			}

			filteredAPIResources = append(filteredAPIResources, r)
		}
		resource.APIResources = filteredAPIResources

		filteredResources = append(filteredResources, resource)
	}

	return filteredGroups, filteredResources, nil
}
