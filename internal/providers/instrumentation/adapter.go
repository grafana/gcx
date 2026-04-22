package instrumentation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	fleetbase "github.com/grafana/gcx/internal/fleet"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"golang.org/x/sync/errgroup"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sunstructured "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ---------------------------------------------------------------------------
// Static descriptors
// ---------------------------------------------------------------------------

//nolint:gochecknoglobals // Static descriptor used in self-registration pattern.
var clusterDescriptorVar = resources.Descriptor{
	GroupVersion: ClusterGVK.GroupVersion(),
	Kind:         "Cluster",
	Singular:     "cluster",
	Plural:       "clusters",
}

//nolint:gochecknoglobals // Static descriptor used in self-registration pattern.
var appDescriptorVar = resources.Descriptor{
	GroupVersion: AppGVK.GroupVersion(),
	Kind:         "App",
	Singular:     "app",
	Plural:       "apps",
}

// ClusterDescriptor returns the static descriptor for Cluster resources.
func ClusterDescriptor() resources.Descriptor { return clusterDescriptorVar }

// AppDescriptor returns the static descriptor for App resources.
func AppDescriptor() resources.Descriptor { return appDescriptorVar }

// ---------------------------------------------------------------------------
// Natural key functions (used by T8 init to call adapter.RegisterNaturalKey)
// ---------------------------------------------------------------------------

// ClusterNaturalKey extracts the natural key for a Cluster from metadata.name.
// Clusters are identified by metadata.name (cluster name), not a spec field,
// so we read the name directly from the object metadata.
//
//nolint:gochecknoglobals // Self-registration pattern (like database/sql drivers).
var ClusterNaturalKey adapter.NaturalKeyExtractor = func(obj *k8sunstructured.Unstructured) (string, bool) {
	name := obj.GetName()
	if name == "" {
		return "", false
	}
	return adapter.SlugifyName(name), true
}

// AppNaturalKey extracts the natural key for an App from spec.cluster + spec.namespace.
var AppNaturalKey = adapter.SpecFieldKey("cluster", "namespace") //nolint:gochecknoglobals

// ---------------------------------------------------------------------------
// Schema and example
// ---------------------------------------------------------------------------

// ClusterSchema returns the JSON Schema for the Cluster resource kind.
func ClusterSchema() json.RawMessage {
	return adapter.SchemaFromType[Cluster](clusterDescriptorVar)
}

// AppSchema returns the JSON Schema for the App resource kind.
func AppSchema() json.RawMessage {
	return adapter.SchemaFromType[App](appDescriptorVar)
}

// ClusterExample returns an example manifest for the Cluster resource kind.
func ClusterExample() json.RawMessage {
	return json.RawMessage(`{
  "apiVersion": "instrumentation.grafana.app/v1alpha1",
  "kind": "Cluster",
  "metadata": {"name": "prod-east"},
  "spec": {
    "costMetrics": true,
    "clusterEvents": true,
    "energyMetrics": false,
    "nodeLogs": true
  }
}`)
}

// AppExample returns an example manifest for the App resource kind.
func AppExample() json.RawMessage {
	return json.RawMessage(`{
  "apiVersion": "instrumentation.grafana.app/v1alpha1",
  "kind": "App",
  "metadata": {"name": "prod-east-payments"},
  "spec": {
    "cluster": "prod-east",
    "namespace": "payments",
    "selection": "all",
    "tracing": true,
    "logging": true,
    "apps": [
      {"name": "checkout", "selection": "labeled", "type": "java"}
    ]
  }
}`)
}

// ---------------------------------------------------------------------------
// Registrations — package-level constructor
// ---------------------------------------------------------------------------

// Registrations returns the adapter.Registration entries for Cluster and App.
// Called by the provider's TypedRegistrations() method.
func Registrations() []adapter.Registration {
	loader := &providers.ConfigLoader{}
	return []adapter.Registration{
		{
			Factory:    newClusterFactory(loader),
			Descriptor: ClusterDescriptor(),
			GVK:        ClusterGVK,
			Schema:     ClusterSchema(),
			Example:    ClusterExample(),
		},
		{
			Factory:    newAppFactory(loader),
			Descriptor: AppDescriptor(),
			GVK:        AppGVK,
			Schema:     AppSchema(),
			Example:    AppExample(),
		},
	}
}

// loadInstrumentation loads the fleet client and derives the Client, PromHeaders,
// and BackendURLs used by all CRUD factories. Extracted to eliminate the repeated
// three-step preamble across newClusterFactory, newAppFactory, NewClusterTypedCRUD,
// and NewAppTypedCRUD.
func loadInstrumentation(ctx context.Context, loader fleetbase.ConfigLoader) (*Client, PromHeaders, BackendURLs, error) {
	r, err := fleetbase.LoadClientWithStack(ctx, loader)
	if err != nil {
		return nil, PromHeaders{}, BackendURLs{}, fmt.Errorf("instrumentation: load fleet client: %w", err)
	}
	return NewClient(r.Client), PromHeadersFromStack(r.Stack), BackendURLsFromStack(r.Stack), nil
}

// ---------------------------------------------------------------------------
// NewClusterTypedCRUD creates a TypedCRUD for Cluster resources using the provided loader.
// Called by clusters/command.go for CLI list/get/create/update/delete operations.
// The adapter registration pipeline uses newClusterFactory (unexported) instead.
func NewClusterTypedCRUD(ctx context.Context, loader fleetbase.ConfigLoader) (*adapter.TypedCRUD[Cluster], error) {
	client, promHdrs, urls, err := loadInstrumentation(ctx, loader)
	if err != nil {
		return nil, err
	}
	return &adapter.TypedCRUD[Cluster]{
		ListFn:     clusterListFn(client, promHdrs),
		GetFn:      clusterGetFn(client),
		CreateFn:   clusterWriteFn(client, urls),
		UpdateFn:   clusterUpdateFn(client, urls),
		DeleteFn:   clusterDeleteFn(client, urls),
		Descriptor: clusterDescriptorVar,
		Aliases:    []string{"cluster"},
	}, nil
}

// NewAppTypedCRUD creates a TypedCRUD for App resources using the provided loader.
// Called by apps/command.go for CLI list/get/create/update/delete operations.
// Identity enforcement (FR-006) is handled by validateAppIdentity inside appAdapter.
func NewAppTypedCRUD(ctx context.Context, loader fleetbase.ConfigLoader) (*adapter.TypedCRUD[App], error) {
	client, promHdrs, urls, err := loadInstrumentation(ctx, loader)
	if err != nil {
		return nil, err
	}
	return &adapter.TypedCRUD[App]{
		ListFn:     appListFn(client, promHdrs),
		GetFn:      nil, // Falls back to ListFn + client-side filter by GetResourceName().
		CreateFn:   appWriteFn(client, urls),
		UpdateFn:   appUpdateFn(client, urls),
		DeleteFn:   appDeleteFn(client, promHdrs, urls),
		Descriptor: appDescriptorVar,
		Aliases:    []string{"app"},
	}, nil
}

// ValidateAppIdentityName checks that a candidate metadata.name (from a YAML manifest) matches
// AppDisplayName(cluster, namespace). Empty name is valid (identity inferred from spec fields).
func ValidateAppIdentityName(metadataName, cluster, namespace string) error {
	if metadataName == "" {
		return nil
	}
	expected := AppDisplayName(cluster, namespace)
	if metadataName != expected {
		return fmt.Errorf(
			"instrumentation: App identity mismatch: metadata.name=%q does not match AppDisplayName(spec.cluster=%q, spec.namespace=%q)=%q; "+
				"either omit metadata.name or set it to %q",
			metadataName, cluster, namespace, expected, expected,
		)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Cluster factory
// ---------------------------------------------------------------------------

func newClusterFactory(loader fleetbase.ConfigLoader) adapter.Factory {
	return func(ctx context.Context) (adapter.ResourceAdapter, error) {
		client, promHdrs, urls, err := loadInstrumentation(ctx, loader)
		if err != nil {
			return nil, err
		}
		crud := &adapter.TypedCRUD[Cluster]{
			ListFn:     clusterListFn(client, promHdrs),
			GetFn:      clusterGetFn(client),
			CreateFn:   clusterWriteFn(client, urls),
			UpdateFn:   clusterUpdateFn(client, urls),
			DeleteFn:   clusterDeleteFn(client, urls),
			Descriptor: clusterDescriptorVar,
			Aliases:    []string{"cluster"},
		}
		return crud.AsAdapter(), nil
	}
}

func clusterListFn(client *Client, promHdrs PromHeaders) func(ctx context.Context, limit int64) ([]Cluster, error) {
	return func(ctx context.Context, limit int64) ([]Cluster, error) {
		monitoring, err := client.RunK8sMonitoring(ctx, promHdrs)
		if err != nil {
			return nil, fmt.Errorf("instrumentation: list clusters: %w", err)
		}

		clusters := monitoring.Clusters
		if limit > 0 && int64(len(clusters)) > limit {
			clusters = clusters[:limit]
		}

		results := make([]Cluster, len(clusters))
		g, gctx := errgroup.WithContext(ctx)
		g.SetLimit(10)
		for i, cs := range clusters {
			name := cs.Name
			idx := i
			state := cs
			g.Go(func() error {
				resp, err := client.GetK8SInstrumentation(gctx, name)
				if err != nil {
					return fmt.Errorf("instrumentation: get cluster %q: %w", name, err)
				}
				results[idx] = responseToCluster(name, resp, &state)
				return nil
			})
		}
		if err := g.Wait(); err != nil {
			return nil, err
		}
		return results, nil
	}
}

func clusterGetFn(client *Client) func(ctx context.Context, name string) (*Cluster, error) {
	return func(ctx context.Context, name string) (*Cluster, error) {
		resp, err := client.GetK8SInstrumentation(ctx, name)
		if err != nil {
			// Propagate err: client.GetK8SInstrumentation wraps 404s with
			// adapter.ErrNotFound, so %w preserves the not-found sentinel
			// while surfacing real errors (network, 5xx, auth, decode) to callers.
			return nil, fmt.Errorf("instrumentation: get cluster %q: %w", name, err)
		}
		c := responseToCluster(name, resp, nil)
		return &c, nil
	}
}

func clusterDeleteFn(client *Client, urls BackendURLs) func(ctx context.Context, name string) error {
	return func(ctx context.Context, name string) error {
		// Delete = zero out all K8S monitoring flags, bypassing the optimistic lock.
		if err := client.SetK8SInstrumentation(ctx, name, K8sSpec{}, urls); err != nil {
			return fmt.Errorf("instrumentation: delete cluster %q: %w", name, err)
		}
		return nil
	}
}

func clusterWriteFn(client *Client, urls BackendURLs) func(ctx context.Context, c *Cluster) (*Cluster, error) {
	return func(ctx context.Context, c *Cluster) (*Cluster, error) {
		remote, err := client.GetK8SInstrumentation(ctx, c.GetResourceName())
		if err != nil {
			// On Create, not-found is fine — no remote state to conflict with.
			if !errors.Is(err, adapter.ErrNotFound) {
				return nil, fmt.Errorf("instrumentation: fetch remote cluster state: %w", err)
			}
		} else {
			remoteCluster := responseToCluster(c.GetResourceName(), remote, nil)
			if flags := remoteOnlyClusterFlags(*c, remoteCluster); len(flags) > 0 {
				return nil, fmt.Errorf("instrumentation: optimistic lock: remote Cluster %q has flags not in local manifest: %s",
					c.GetResourceName(), strings.Join(flags, ", "))
			}
			// Preserve the remote Selection when the local manifest omits it.
			if c.Selection == "" && remoteCluster.Selection != "" {
				c.Selection = remoteCluster.Selection
			}
		}
		if err := client.SetK8SInstrumentation(ctx, c.GetResourceName(), clusterToK8sSpec(*c), urls); err != nil {
			return nil, fmt.Errorf("instrumentation: set cluster %q: %w", c.GetResourceName(), err)
		}
		return c, nil
	}
}

func clusterUpdateFn(client *Client, urls BackendURLs) func(ctx context.Context, name string, c *Cluster) (*Cluster, error) {
	return func(ctx context.Context, name string, c *Cluster) (*Cluster, error) {
		c.SetResourceName(name)
		remote, err := client.GetK8SInstrumentation(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("instrumentation: fetch remote cluster state: %w", err)
		}
		remoteCluster := responseToCluster(name, remote, nil)
		if flags := remoteOnlyClusterFlags(*c, remoteCluster); len(flags) > 0 {
			return nil, fmt.Errorf("instrumentation: optimistic lock: remote Cluster %q has flags not in local manifest: %s",
				name, strings.Join(flags, ", "))
		}
		// Preserve the remote Selection when the local manifest omits it.
		if c.Selection == "" && remoteCluster.Selection != "" {
			c.Selection = remoteCluster.Selection
		}
		if err := client.SetK8SInstrumentation(ctx, name, clusterToK8sSpec(*c), urls); err != nil {
			return nil, fmt.Errorf("instrumentation: set cluster %q: %w", name, err)
		}
		return c, nil
	}
}

// ---------------------------------------------------------------------------
// App factory
// ---------------------------------------------------------------------------

func newAppFactory(loader fleetbase.ConfigLoader) adapter.Factory {
	return func(ctx context.Context) (adapter.ResourceAdapter, error) {
		client, promHdrs, urls, err := loadInstrumentation(ctx, loader)
		if err != nil {
			return nil, err
		}
		crud := &adapter.TypedCRUD[App]{
			ListFn:     appListFn(client, promHdrs),
			GetFn:      nil, // Falls back to ListFn + client-side filter by GetResourceName().
			CreateFn:   appWriteFn(client, urls),
			UpdateFn:   appUpdateFn(client, urls),
			DeleteFn:   appDeleteFn(client, promHdrs, urls),
			Descriptor: appDescriptorVar,
			Aliases:    []string{"app"},
		}
		return &appAdapter{inner: crud.AsAdapter()}, nil
	}
}

func appListFn(client *Client, promHdrs PromHeaders) func(ctx context.Context, limit int64) ([]App, error) {
	return func(ctx context.Context, limit int64) ([]App, error) {
		monitoring, err := client.RunK8sMonitoring(ctx, promHdrs)
		if err != nil {
			return nil, fmt.Errorf("instrumentation: list apps: %w", err)
		}

		type clusterApps struct {
			clusterName string
			apps        []App
		}

		perCluster := make([]clusterApps, len(monitoring.Clusters))
		g, gctx := errgroup.WithContext(ctx)
		g.SetLimit(10)
		for i, cs := range monitoring.Clusters {
			name := cs.Name
			idx := i
			g.Go(func() error {
				resp, err := client.GetAppInstrumentation(gctx, name)
				if err != nil {
					return fmt.Errorf("instrumentation: get apps for cluster %q: %w", name, err)
				}
				apps := make([]App, 0, len(resp.Namespaces))
				for _, ns := range resp.Namespaces {
					apps = append(apps, namespaceConfigToApp(name, ns))
				}
				perCluster[idx] = clusterApps{clusterName: name, apps: apps}
				return nil
			})
		}
		if err := g.Wait(); err != nil {
			return nil, err
		}

		var all []App
		for _, ca := range perCluster {
			all = append(all, ca.apps...)
		}
		return adapter.TruncateSlice(all, limit), nil
	}
}

func appDeleteFn(client *Client, promHdrs PromHeaders, urls BackendURLs) func(ctx context.Context, name string) error {
	return func(ctx context.Context, name string) error {
		// Find the app by listing all apps and matching on display name.
		// Identity (cluster+namespace) comes from the server response, not parsed from name.
		listFn := appListFn(client, promHdrs)
		all, err := listFn(ctx, 0)
		if err != nil {
			return fmt.Errorf("instrumentation: delete app %q: list: %w", name, err)
		}
		var found *App
		for i := range all {
			if all[i].GetResourceName() == name {
				found = &all[i]
				break
			}
		}
		if found == nil {
			return fmt.Errorf("instrumentation: app %q not found", name)
		}
		// Read the full namespace list for the cluster, remove the target, write back.
		// Bypasses the optimistic lock — intentional deletion.
		// By design: concurrent remote changes between the list and the set will be silently overwritten.
		remote, err := client.GetAppInstrumentation(ctx, found.Cluster)
		if err != nil {
			return fmt.Errorf("instrumentation: delete app %q: get cluster %q: %w", name, found.Cluster, err)
		}
		remaining := make([]NamespaceConfig, 0, len(remote.Namespaces))
		for _, ns := range remote.Namespaces {
			if ns.Name != found.Namespace {
				remaining = append(remaining, ns)
			}
		}
		if err := client.SetAppInstrumentation(ctx, found.Cluster, remaining, urls); err != nil {
			return fmt.Errorf("instrumentation: delete app %q: %w", name, err)
		}
		return nil
	}
}

func appWriteFn(client *Client, urls BackendURLs) func(ctx context.Context, app *App) (*App, error) {
	return func(ctx context.Context, app *App) (*App, error) {
		return applyApp(ctx, client, urls, app)
	}
}

func appUpdateFn(client *Client, urls BackendURLs) func(ctx context.Context, name string, app *App) (*App, error) {
	return func(ctx context.Context, name string, app *App) (*App, error) {
		// Guard against the silent wrong-app bug: when callers supply a name
		// (either the CLI positional arg or metadata.name in a manifest), it
		// must match AppDisplayName(spec.cluster, spec.namespace). Otherwise
		// the CLI arg is discarded silently while applyApp keys off spec fields.
		//
		// An empty name means "derive from spec" — used by the push pipeline
		// when a manifest omits metadata.name (validated separately by
		// validateAppIdentity in appAdapter before reaching this function).
		if name != "" {
			expected := AppDisplayName(app.Cluster, app.Namespace)
			if name != expected {
				return nil, fmt.Errorf(
					"instrumentation: App identity mismatch: name %q does not match AppDisplayName(spec.cluster=%q, spec.namespace=%q)=%q",
					name, app.Cluster, app.Namespace, expected,
				)
			}
		}
		return applyApp(ctx, client, urls, app)
	}
}

// applyApp performs the read-modify-write for App creates and updates.
func applyApp(ctx context.Context, client *Client, urls BackendURLs, app *App) (*App, error) {
	// Fetch current remote state for the cluster.
	remote, err := client.GetAppInstrumentation(ctx, app.Cluster)
	if err != nil {
		return nil, fmt.Errorf("instrumentation: fetch remote app state for cluster %q: %w", app.Cluster, err)
	}

	// Optimistic lock: check for remote-only apps in the target namespace.
	remoteSpec := &AppSpec{Namespaces: remote.Namespaces}
	localNS := appToNamespaceConfig(*app)
	diff := CompareNamespace(remoteSpec, &localNS)
	if !diff.IsEmpty() {
		var msgs []string
		for _, a := range diff.Apps {
			msgs = append(msgs, fmt.Sprintf("app %q in namespace %q", a.App, a.Namespace))
		}
		return nil, fmt.Errorf("instrumentation: optimistic lock: remote has items absent from local manifest: %s",
			strings.Join(msgs, "; "))
	}

	// Merge: replace or append the target namespace entry.
	merged := mergeNamespace(remote.Namespaces, localNS)

	if err := client.SetAppInstrumentation(ctx, app.Cluster, merged, urls); err != nil {
		return nil, fmt.Errorf("instrumentation: set app instrumentation for cluster %q: %w", app.Cluster, err)
	}
	return app, nil
}

// mergeNamespace replaces the entry for localNS.Name in namespaces, or appends if absent.
func mergeNamespace(namespaces []NamespaceConfig, localNS NamespaceConfig) []NamespaceConfig {
	for i, ns := range namespaces {
		if ns.Name == localNS.Name {
			namespaces[i] = localNS
			return namespaces
		}
	}
	return append(namespaces, localNS)
}

// ---------------------------------------------------------------------------
// appAdapter — wraps the inner adapter to enforce App identity contract
// ---------------------------------------------------------------------------

// appAdapter wraps a ResourceAdapter and validates App identity before Create/Update.
// This is required because typedAdapter.Create() discards metadata.name before
// invoking CreateFn, so the identity check must happen at the ResourceAdapter
// level where the full *k8sunstructured.Unstructured is still available.
type appAdapter struct {
	inner adapter.ResourceAdapter
}

func (a *appAdapter) List(ctx context.Context, opts metav1.ListOptions) (*k8sunstructured.UnstructuredList, error) {
	return a.inner.List(ctx, opts)
}

func (a *appAdapter) Get(ctx context.Context, name string, opts metav1.GetOptions) (*k8sunstructured.Unstructured, error) {
	return a.inner.Get(ctx, name, opts)
}

func (a *appAdapter) Create(ctx context.Context, obj *k8sunstructured.Unstructured, opts metav1.CreateOptions) (*k8sunstructured.Unstructured, error) {
	if err := validateAppIdentity(obj); err != nil {
		return nil, err
	}
	return a.inner.Create(ctx, obj, opts)
}

func (a *appAdapter) Update(ctx context.Context, obj *k8sunstructured.Unstructured, opts metav1.UpdateOptions) (*k8sunstructured.Unstructured, error) {
	if err := validateAppIdentity(obj); err != nil {
		return nil, err
	}
	return a.inner.Update(ctx, obj, opts)
}

func (a *appAdapter) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	return a.inner.Delete(ctx, name, opts)
}

func (a *appAdapter) Descriptor() resources.Descriptor { return a.inner.Descriptor() }
func (a *appAdapter) Aliases() []string                { return a.inner.Aliases() }
func (a *appAdapter) Schema() json.RawMessage          { return a.inner.Schema() }
func (a *appAdapter) Example() json.RawMessage         { return a.inner.Example() }

// validateAppIdentity checks that metadata.name, when present, matches
// AppDisplayName(spec.cluster, spec.namespace). Absent name is valid.
func validateAppIdentity(obj *k8sunstructured.Unstructured) error {
	name := obj.GetName()
	if name == "" {
		return nil
	}

	cluster, _, _ := k8sunstructured.NestedString(obj.Object, "spec", "cluster")
	namespace, _, _ := k8sunstructured.NestedString(obj.Object, "spec", "namespace")
	expected := AppDisplayName(cluster, namespace)

	if name != expected {
		return fmt.Errorf(
			"instrumentation: App identity mismatch: metadata.name=%q does not match AppDisplayName(spec.cluster=%q, spec.namespace=%q)=%q",
			name, cluster, namespace, expected,
		)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Conversion helpers
// ---------------------------------------------------------------------------

func responseToCluster(name string, r *GetK8SInstrumentationResponse, cs *ClusterState) Cluster {
	c := Cluster{
		Selection:     r.Selection,
		CostMetrics:   r.CostMetrics,
		ClusterEvents: r.ClusterEvents,
		EnergyMetrics: r.EnergyMetrics,
		NodeLogs:      r.NodeLogs,
	}
	c.SetResourceName(name)
	if cs != nil {
		c.Status = cs.InstrumentationStatus
		c.Workloads = cs.Workloads
		c.Pods = cs.Pods
		c.Nodes = cs.Nodes
	}
	return c
}

func clusterToK8sSpec(c Cluster) K8sSpec {
	return K8sSpec{
		Selection:     c.Selection,
		CostMetrics:   c.CostMetrics,
		ClusterEvents: c.ClusterEvents,
		EnergyMetrics: c.EnergyMetrics,
		NodeLogs:      c.NodeLogs,
	}
}

func namespaceConfigToApp(clusterName string, ns NamespaceConfig) App {
	apps := make([]AppConfig, len(ns.Apps))
	for i, a := range ns.Apps {
		apps[i] = AppConfig{Name: a.Name, Selection: a.Selection, Type: a.Type}
	}
	return App{
		Cluster:         clusterName,
		Namespace:       ns.Name,
		Selection:       ns.Selection,
		Tracing:         ns.Tracing,
		Logging:         ns.Logging,
		ProcessMetrics:  ns.ProcessMetrics,
		ExtendedMetrics: ns.ExtendedMetrics,
		Profiling:       ns.Profiling,
		Apps:            apps,
	}
}

func appToNamespaceConfig(app App) NamespaceConfig {
	apps := make([]AppConfig, len(app.Apps))
	for i, a := range app.Apps {
		apps[i] = AppConfig{Name: a.Name, Selection: a.Selection, Type: a.Type}
	}
	return NamespaceConfig{
		Name:            app.Namespace,
		Selection:       app.Selection,
		Tracing:         app.Tracing,
		Logging:         app.Logging,
		ProcessMetrics:  app.ProcessMetrics,
		ExtendedMetrics: app.ExtendedMetrics,
		Profiling:       app.Profiling,
		Apps:            apps,
	}
}

// remoteOnlyClusterFlags returns the names of K8s flags that are enabled in
// the remote cluster but disabled (false) in the local manifest.
func remoteOnlyClusterFlags(local, remote Cluster) []string {
	var flags []string
	if remote.CostMetrics && !local.CostMetrics {
		flags = append(flags, "costMetrics")
	}
	if remote.ClusterEvents && !local.ClusterEvents {
		flags = append(flags, "clusterEvents")
	}
	if remote.EnergyMetrics && !local.EnergyMetrics {
		flags = append(flags, "energyMetrics")
	}
	if remote.NodeLogs && !local.NodeLogs {
		flags = append(flags, "nodeLogs")
	}
	return flags
}

// Ensure appAdapter satisfies adapter.ResourceAdapter at compile time.
var _ adapter.ResourceAdapter = (*appAdapter)(nil)
