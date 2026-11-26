package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	apidiscovery "k8s.io/api/apidiscovery/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
)

// The discovery client here replaces client-go's disk-cached discovery client,
// whose package imports k8s.io/client-go/kubernetes/scheme: registering every
// built-in Kubernetes type costs each gcx process tens of milliseconds at
// startup, for types gcx never decodes. It keeps that client's behaviour and
// on-disk cache layout, so existing caches stay valid:
//
//   - /api and /apis are requested as aggregated discovery (one request each,
//     groups and resources together), falling back to the legacy format, where
//     each group version's resources are fetched separately.
//   - A 404 from /api means the server has no legacy core group (Grafana).
//   - Stale aggregated group versions are dropped. A group version that fails
//     to load fails discovery as a whole.
//   - servergroups.json and <group>/<version>/serverresources.json under the
//     cache directory are reused until they are defaultDiscoveryCacheTTL
//     old; empty results aren't cached, and failing to write the cache isn't
//     an error.

const (
	acceptAggregated = "application/json;g=apidiscovery.k8s.io;v=v2;as=APIGroupDiscoveryList,application/json"
	// defaultDiscoveryTimeout bounds each request when the config sets no timeout.
	defaultDiscoveryTimeout = 32 * time.Second
)

// cachedClient discovers a server's API groups and resources, caching the
// results on disk.
type cachedClient struct {
	baseURL string
	http    *http.Client
	dir     string
}

func newCachedClient(cfg *rest.Config, dir string) (*cachedClient, error) {
	cfg = rest.CopyConfig(cfg)
	if cfg.Timeout == 0 {
		cfg.Timeout = defaultDiscoveryTimeout
	}
	httpClient, err := rest.HTTPClientFor(cfg)
	if err != nil {
		return nil, fmt.Errorf("creating HTTP client: %w", err)
	}
	return &cachedClient{baseURL: strings.TrimSuffix(cfg.Host, "/"), http: httpClient, dir: dir}, nil
}

// ServerGroupsAndResources returns the server's API groups and the resources
// of each of their versions.
func (c *cachedClient) ServerGroupsAndResources() ([]*metav1.APIGroup, []*metav1.APIResourceList, error) {
	ctx := context.Background()
	groupsFile := filepath.Join(c.dir, "servergroups.json")

	var groups metav1.APIGroupList
	var live map[string]*metav1.APIResourceList // from aggregated discovery
	if !c.readCache(groupsFile, &groups) {
		var err error
		groups, live, err = c.fetchGroups(ctx)
		if err != nil {
			return nil, nil, err
		}
		if len(groups.Groups) > 0 {
			groups.Kind, groups.APIVersion = "APIGroupList", "v1"
			c.writeCache(groupsFile, groups)
		}
	}

	apiGroups := make([]*metav1.APIGroup, 0, len(groups.Groups))
	byGV := map[string]*metav1.APIResourceList{}
	var misses []string
	for i := range groups.Groups {
		apiGroups = append(apiGroups, &groups.Groups[i])
		for _, v := range groups.Groups[i].Versions {
			var list metav1.APIResourceList
			if c.readCache(filepath.Join(c.dir, v.GroupVersion, "serverresources.json"), &list) {
				byGV[v.GroupVersion] = &list
			} else {
				misses = append(misses, v.GroupVersion)
			}
		}
	}
	if len(misses) > 0 && live == nil {
		// One aggregated request answers every miss, if the server supports it.
		var err error
		if _, live, err = c.fetchGroups(ctx); err != nil {
			return nil, nil, err
		}
	}

	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		failed []error
	)
	for _, gv := range misses {
		wg.Go(func() {
			list, err := c.resources(ctx, gv, live)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed = append(failed, fmt.Errorf("%s: %w", gv, err))
				return
			}
			byGV[gv] = list
		})
	}
	wg.Wait()
	if len(failed) > 0 {
		return nil, nil, fmt.Errorf("unable to retrieve the complete list of server APIs: %w", errors.Join(failed...))
	}

	lists := make([]*metav1.APIResourceList, 0, len(byGV))
	for _, g := range groups.Groups {
		for _, v := range g.Versions {
			if list, ok := byGV[v.GroupVersion]; ok {
				lists = append(lists, list)
			}
		}
	}
	return apiGroups, lists, nil
}

// resources returns a group version's resources from live aggregated results
// or, for a server without aggregated discovery, from the server, and caches them.
func (c *cachedClient) resources(ctx context.Context, gv string, live map[string]*metav1.APIResourceList) (*metav1.APIResourceList, error) {
	fetched, ok := live[gv]
	if !ok {
		path := "/apis/" + gv
		if !strings.Contains(gv, "/") {
			path = "/api/" + gv // the legacy core group
		}
		fetched = &metav1.APIResourceList{}
		if _, err := c.get(ctx, path, "application/json", fetched); err != nil {
			return nil, err
		}
	}
	if len(fetched.APIResources) > 0 {
		cached := *fetched
		cached.Kind, cached.APIVersion = "APIResourceList", "v1"
		c.writeCache(filepath.Join(c.dir, gv, "serverresources.json"), cached)
	}
	return fetched, nil
}

// fetchGroups requests /api and /apis, returning the groups and, when the
// server answered with aggregated discovery, every group version's resources.
func (c *cachedClient) fetchGroups(ctx context.Context) (metav1.APIGroupList, map[string]*metav1.APIResourceList, error) {
	var groups metav1.APIGroupList
	resources := map[string]*metav1.APIResourceList{}
	for _, path := range []string{"/api", "/apis"} {
		var body json.RawMessage
		aggregated, err := c.get(ctx, path, acceptAggregated, &body)
		if path == "/api" && apierrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return groups, nil, err
		}
		switch {
		case aggregated:
			var list apidiscovery.APIGroupDiscoveryList
			if err := json.Unmarshal(body, &list); err != nil {
				return groups, nil, fmt.Errorf("decoding %s: %w", path, err)
			}
			for _, g := range list.Items {
				groups.Groups = append(groups.Groups, convertAPIGroup(g, resources))
			}
		case path == "/api":
			var versions metav1.APIVersions
			if err := json.Unmarshal(body, &versions); err != nil {
				return groups, nil, fmt.Errorf("decoding %s: %w", path, err)
			}
			var core metav1.APIGroup
			for _, v := range versions.Versions {
				core.Versions = append(core.Versions, metav1.GroupVersionForDiscovery{GroupVersion: v, Version: v})
			}
			if len(core.Versions) > 0 {
				core.PreferredVersion = core.Versions[0]
			}
			groups.Groups = append(groups.Groups, core)
		default:
			var list metav1.APIGroupList
			if err := json.Unmarshal(body, &list); err != nil {
				return groups, nil, fmt.Errorf("decoding %s: %w", path, err)
			}
			groups.Groups = append(groups.Groups, list.Groups...)
		}
	}
	return groups, resources, nil
}

// get requests path and decodes its JSON body into out, reporting whether the
// server answered with aggregated discovery. Error statuses become Kubernetes
// API status errors, as from client-go.
func (c *cachedClient) get(ctx context.Context, path, accept string, out any) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Accept", accept)
	resp, err := c.http.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var status metav1.Status
		if json.Unmarshal(body, &status) == nil && status.Kind == "Status" {
			return false, &apierrors.StatusError{ErrStatus: status}
		}
		return false, apierrors.NewGenericServerResponse(resp.StatusCode, http.MethodGet, schema.GroupResource{}, "", strings.TrimSpace(string(body)), 0, true)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return false, fmt.Errorf("decoding %s: %w", path, err)
	}
	_, params, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	return params["g"] == "apidiscovery.k8s.io" && params["v"] == "v2" && params["as"] == "APIGroupDiscoveryList", nil
}

// convertAPIGroup converts an aggregated discovery group, adding its versions'
// resources to resources. Stale versions are left out.
func convertAPIGroup(g apidiscovery.APIGroupDiscovery, resources map[string]*metav1.APIResourceList) metav1.APIGroup {
	group := metav1.APIGroup{Name: g.Name}
	for _, v := range g.Versions {
		if v.Freshness == apidiscovery.DiscoveryFreshnessStale {
			continue
		}
		gv := schema.GroupVersion{Group: g.Name, Version: v.Version}.String()
		version := metav1.GroupVersionForDiscovery{GroupVersion: gv, Version: v.Version}
		group.Versions = append(group.Versions, version)
		if group.PreferredVersion == (metav1.GroupVersionForDiscovery{}) {
			group.PreferredVersion = version
		}
		list := &metav1.APIResourceList{GroupVersion: gv}
		for _, r := range v.Resources {
			parent := metav1.APIResource{
				Name:         r.Resource,
				SingularName: r.SingularResource,
				Namespaced:   r.Scope == apidiscovery.ScopeNamespace,
				Verbs:        r.Verbs,
				ShortNames:   r.ShortNames,
				Categories:   r.Categories,
			}
			// A resource without a kind is left out, but not its subresources.
			if r.ResponseKind != nil && *r.ResponseKind != (metav1.GroupVersionKind{}) {
				parent.Group, parent.Version, parent.Kind = r.ResponseKind.Group, r.ResponseKind.Version, r.ResponseKind.Kind
				list.APIResources = append(list.APIResources, parent)
			}
			for _, s := range r.Subresources {
				if s.ResponseKind == nil || *s.ResponseKind == (metav1.GroupVersionKind{}) {
					continue
				}
				list.APIResources = append(list.APIResources, metav1.APIResource{
					Name:         parent.Name + "/" + s.Subresource,
					SingularName: parent.SingularName,
					Namespaced:   parent.Namespaced,
					Group:        s.ResponseKind.Group,
					Version:      s.ResponseKind.Version,
					Kind:         s.ResponseKind.Kind,
					Verbs:        s.Verbs,
				})
			}
		}
		resources[gv] = list
	}
	return group
}

// readCache decodes file into out if it exists and is younger than defaultDiscoveryCacheTTL.
func (c *cachedClient) readCache(file string, out any) bool {
	info, err := os.Stat(file)
	if err != nil || time.Since(info.ModTime()) > defaultDiscoveryCacheTTL {
		return false
	}
	data, err := os.ReadFile(file)
	return err == nil && json.Unmarshal(data, out) == nil
}

// writeCache atomically replaces file with v. Failures are ignored: the cache
// is an optimization, and some environments (the wasm sandbox) can't write it.
func (c *cachedClient) writeCache(file string, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o750); err != nil {
		return
	}
	f, err := os.CreateTemp(filepath.Dir(file), filepath.Base(file)+".")
	if err != nil {
		return
	}
	defer os.Remove(f.Name())
	_, werr := f.Write(data)
	if cerr := f.Close(); werr != nil || cerr != nil {
		return
	}
	_ = os.Chmod(f.Name(), 0o660)
	_ = os.Rename(f.Name(), file)
}
