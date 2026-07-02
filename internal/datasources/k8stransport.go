package datasources

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/httputils"
	"k8s.io/client-go/rest"
)

// errK8sNotServed signals that the app-platform datasource API did not serve a
// request (the per-plugin group is absent, the discovery probe found no
// datasource groups, or a subresource such as health is not registered on this
// stack). dualTransport treats it as the cue to fall back to the legacy REST
// client; it is never surfaced to callers.
var errK8sNotServed = errors.New("app-platform datasource API not served")

// k8sTransport implements Transport against Grafana's app-platform datasource
// API: /apis/{pluginID}.datasource.grafana.app/v0alpha1/namespaces/{ns}/datasources/...
//
// The surface is per-plugin-group partitioned, so write operations derive the
// group from the datasource type (known from the manifest) while UID-addressed
// reads resolve it via the discovery-backed index in servedGroupCache.
type k8sTransport struct {
	host       string
	namespace  string
	httpClient *http.Client
	maxBytes   int64
	groups     *servedGroupCache
}

var _ Transport = (*k8sTransport)(nil)

func newK8sTransport(cfg config.NamespacedRESTConfig) (*k8sTransport, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}
	t := &k8sTransport{
		host:       cfg.Host,
		namespace:  cfg.Namespace,
		httpClient: httpClient,
		maxBytes:   maxResponseBytes,
	}
	t.groups = newServedGroupCache(t)
	return t, nil
}

// k8sDataSource is the app-platform DataSource envelope. metadata.name is the
// datasource UID; secrets live in a top-level `secure` block keyed by name.
type k8sDataSource struct {
	APIVersion string                  `json:"apiVersion"`
	Kind       string                  `json:"kind"`
	Metadata   k8sMetadata             `json:"metadata"`
	Spec       map[string]any          `json:"spec,omitempty"`
	Secure     map[string]inlineSecure `json:"secure,omitempty"`
}

type k8sMetadata struct {
	Name            string `json:"name,omitempty"`
	Namespace       string `json:"namespace,omitempty"`
	ResourceVersion string `json:"resourceVersion,omitempty"`
}

// inlineSecure mirrors the app-platform InlineSecureValue. `create` carries a
// write-only value; `name` is the read-back reference (value never returned).
type inlineSecure struct {
	Create string `json:"create,omitempty"`
	Name   string `json:"name,omitempty"`
}

type k8sList struct {
	Items []k8sDataSource `json:"items"`
}

func (t *k8sTransport) collectionPath(pluginID string) string {
	return fmt.Sprintf("/apis/%s/%s/namespaces/%s/datasources",
		GroupForPluginID(pluginID), datasourceAPIVersion, t.namespace)
}

func (t *k8sTransport) itemPath(pluginID, uid string) string {
	return t.collectionPath(pluginID) + "/" + url.PathEscape(uid)
}

func (t *k8sTransport) healthPath(pluginID, uid string) string {
	return t.itemPath(pluginID, uid) + "/health"
}

// served reports whether any per-plugin datasource group is served on this
// stack. A discovery error is treated as "not served" so the dual transport
// falls back to the always-available legacy REST API.
func (t *k8sTransport) served(ctx context.Context) bool {
	plugins, err := t.groups.servedPlugins(ctx)
	return err == nil && len(plugins) > 0
}

// do issues a request to the app-platform API and returns the HTTP status code
// alongside the body, so callers can branch on 404 (a routing signal) without it
// being a transport error. Only network/encode failures are returned as errors.
func (t *k8sTransport) do(ctx context.Context, method, path string, payload any) (int, []byte, error) {
	var reader io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return 0, nil, fmt.Errorf("failed to encode request: %w", err)
		}
		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, t.host+path, reader)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to create request: %w", err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	body, err := httputils.ReadResponseBody(resp.Body, t.maxBytes)
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("failed to read response: %w", err)
	}
	return resp.StatusCode, body, nil
}

// List aggregates the datasources across every served per-plugin group. When a
// group is not accessible (the user lacks RBAC for it), the app-platform listing
// would be incomplete, so it falls back to the permission-aware legacy REST list
// rather than returning a partial view.
func (t *k8sTransport) List(ctx context.Context) ([]*Datasource, error) {
	out, incomplete, err := t.listAll(ctx)
	if err != nil {
		return nil, err
	}
	if incomplete {
		return nil, errK8sNotServed
	}
	return out, nil
}

// listAll lists every served per-plugin group and builds the UID→pluginID index
// that resolveGroup relies on. Groups that are not served (404) or not
// accessible (403) are skipped and flagged via the incomplete return so callers
// that need a complete view can fall back to REST; the index is still populated
// from the groups that are accessible.
func (t *k8sTransport) listAll(ctx context.Context) ([]*Datasource, bool, error) {
	plugins, err := t.groups.servedPlugins(ctx)
	if err != nil {
		return nil, false, err
	}
	if len(plugins) == 0 {
		return nil, false, errK8sNotServed
	}

	var out []*Datasource
	var incomplete bool
	index := make(map[string]string)
	for _, pluginID := range plugins {
		status, body, err := t.do(ctx, http.MethodGet, t.collectionPath(pluginID), nil)
		if err != nil {
			return nil, false, err
		}
		if status != http.StatusOK {
			// A served group may be inaccessible (403), gone (404), or backed by
			// a broken/absent plugin (5xx). Any of these makes the app-platform
			// enumeration incomplete: skip the group and flag it so List falls
			// back to the permission-aware legacy list for a complete view.
			incomplete = true
			continue
		}
		var list k8sList
		if err := json.Unmarshal(body, &list); err != nil {
			return nil, false, fmt.Errorf("failed to parse datasources response: %w", err)
		}
		for i := range list.Items {
			ds, err := fromK8s(&list.Items[i])
			if err != nil {
				return nil, false, err
			}
			out = append(out, ds)
			index[ds.UID] = pluginID
		}
	}
	t.groups.setIndex(index)
	return out, incomplete, nil
}

// resolveGroup returns the per-plugin group for a datasource UID, building the
// discovery-backed index on first use. ok=false means the UID is not present on
// the app-platform surface (caller should fall back to REST).
func (t *k8sTransport) resolveGroup(ctx context.Context, uid string) (string, bool, error) {
	if !t.groups.indexed() {
		if _, _, err := t.listAll(ctx); err != nil {
			if errors.Is(err, errK8sNotServed) {
				return "", false, nil
			}
			return "", false, err
		}
	}
	p, found := t.groups.lookup(uid)
	return p, found, nil
}

// GetByUID reads a datasource via the app-platform item path, resolving the
// plugin group from the UID index.
func (t *k8sTransport) GetByUID(ctx context.Context, uid string) (*Datasource, error) {
	pluginID, ok, err := t.resolveGroup(ctx, uid)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errK8sNotServed
	}
	status, body, err := t.do(ctx, http.MethodGet, t.itemPath(pluginID, uid), nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, NewAPIError("get datasource", uid, status, body)
	}
	var k k8sDataSource
	if err := json.Unmarshal(body, &k); err != nil {
		return nil, fmt.Errorf("failed to parse datasource response: %w", err)
	}
	return fromK8s(&k)
}

// Create posts a new datasource to the per-plugin collection (type is known
// from the manifest). A 404 means the group is not served — fall back to REST.
func (t *k8sTransport) Create(ctx context.Context, ds *Datasource) (*Datasource, error) {
	if !t.served(ctx) {
		return nil, errK8sNotServed
	}
	obj, err := toK8s(ds, t.namespace, "")
	if err != nil {
		return nil, err
	}
	status, body, err := t.do(ctx, http.MethodPost, t.collectionPath(ds.Type), obj)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, errK8sNotServed
	}
	if status < 200 || status >= 300 {
		return nil, NewAPIError("create datasource", ds.UID, status, body)
	}
	var k k8sDataSource
	if err := json.Unmarshal(body, &k); err != nil {
		return nil, fmt.Errorf("failed to parse create response: %w", err)
	}
	return fromK8s(&k)
}

// Update full-replaces a datasource. The app-platform PUT requires the current
// metadata.resourceVersion (optimistic concurrency), so it is fetched first.
func (t *k8sTransport) Update(ctx context.Context, uid string, ds *Datasource) (*Datasource, error) {
	if !t.served(ctx) {
		return nil, errK8sNotServed
	}
	status, body, err := t.do(ctx, http.MethodGet, t.itemPath(ds.Type, uid), nil)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		// Group not served, or the object is absent — let REST handle it.
		return nil, errK8sNotServed
	}
	if status != http.StatusOK {
		return nil, NewAPIError("update datasource", uid, status, body)
	}
	var current k8sDataSource
	if err := json.Unmarshal(body, &current); err != nil {
		return nil, fmt.Errorf("failed to parse datasource response: %w", err)
	}

	obj, err := toK8s(ds, t.namespace, current.Metadata.ResourceVersion)
	if err != nil {
		return nil, err
	}
	status, body, err = t.do(ctx, http.MethodPut, t.itemPath(ds.Type, uid), obj)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		// 409 Conflict (stale resourceVersion) surfaces as a typed error; we do
		// not silently re-fetch-and-retry — that would defeat the concurrency guard.
		return nil, NewAPIError("update datasource", uid, status, body)
	}
	var k k8sDataSource
	if err := json.Unmarshal(body, &k); err != nil {
		return nil, fmt.Errorf("failed to parse update response: %w", err)
	}
	return fromK8s(&k)
}

// Delete removes a datasource via the app-platform item path.
func (t *k8sTransport) Delete(ctx context.Context, uid string) error {
	pluginID, ok, err := t.resolveGroup(ctx, uid)
	if err != nil {
		return err
	}
	if !ok {
		return errK8sNotServed
	}
	status, body, err := t.do(ctx, http.MethodDelete, t.itemPath(pluginID, uid), nil)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return NewAPIError("delete datasource", uid, status, body)
	}
	return nil
}

// Health prefers the app-platform health subresource. When that subresource is
// not registered on this stack it 404s; the dual transport then falls back to
// the legacy /api/datasources/uid/{uid}/health endpoint.
func (t *k8sTransport) Health(ctx context.Context, uid string) (*HealthResult, error) {
	pluginID, ok, err := t.resolveGroup(ctx, uid)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errK8sNotServed
	}
	status, body, err := t.do(ctx, http.MethodGet, t.healthPath(pluginID, uid), nil)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, errK8sNotServed
	}
	if status != http.StatusOK {
		return nil, NewAPIError("check datasource health", uid, status, body)
	}
	var payload struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("failed to parse health response: %w", err)
	}
	return &HealthResult{UID: uid, Status: payload.Status, Message: payload.Message}, nil
}

// toK8s renders a wire datasource as the app-platform envelope. The spec reuses
// the shared manifest mapping (which strips the routing-only `type` field), and
// secrets are emitted as inline `create` values.
func toK8s(ds *Datasource, namespace, resourceVersion string) (*k8sDataSource, error) {
	spec, err := specToMap(manifestFromDatasource(ds).Spec)
	if err != nil {
		return nil, err
	}
	var secure map[string]inlineSecure
	if len(ds.SecureJSONData) > 0 {
		secure = make(map[string]inlineSecure, len(ds.SecureJSONData))
		for k, v := range ds.SecureJSONData {
			secure[k] = inlineSecure{Create: v}
		}
	}
	return &k8sDataSource{
		APIVersion: GroupForPluginID(ds.Type) + "/" + datasourceAPIVersion,
		Kind:       datasourceKind,
		Metadata: k8sMetadata{
			Name:            ds.UID,
			Namespace:       namespace,
			ResourceVersion: resourceVersion,
		},
		Spec:   spec,
		Secure: secure,
	}, nil
}

// fromK8s maps an app-platform envelope back to the wire datasource. Secret
// values are never present; configured secrets surface only as secureJsonFields.
func fromK8s(k *k8sDataSource) (*Datasource, error) {
	var spec DataSourceSpec
	if len(k.Spec) > 0 {
		b, err := json.Marshal(k.Spec)
		if err != nil {
			return nil, fmt.Errorf("failed to encode datasource spec: %w", err)
		}
		if err := json.Unmarshal(b, &spec); err != nil {
			return nil, fmt.Errorf("failed to decode datasource spec: %w", err)
		}
	}
	m := &DataSourceManifest{
		APIVersion: k.APIVersion,
		Kind:       k.Kind,
		Metadata:   DataSourceMetadata{Name: k.Metadata.Name},
		Spec:       spec,
	}
	ds := m.ToDatasource()
	if len(k.Secure) > 0 {
		ds.SecureJSONFields = make(map[string]bool, len(k.Secure))
		for key := range k.Secure {
			ds.SecureJSONFields[key] = true
		}
	}
	return ds, nil
}
