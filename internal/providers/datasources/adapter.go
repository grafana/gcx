package datasources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	dsclient "github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// datasourceAdapter bridges the legacy /api/datasources REST client into the
// resources pipeline. Unlike most providers it does not use adapter.TypedCRUD:
// the canonical app-platform DataSource carries a top-level `secure` block
// (sibling of spec) that TypedCRUD's spec-only envelope cannot represent, so the
// adapter maps to/from the shared DataSourceManifest instead — the same shape
// the dedicated `gcx datasources` commands emit (top-level `secure`,
// `spec.title`, per-plugin apiVersion).
type datasourceAdapter struct {
	client    dsclient.Transport
	namespace string
	desc      resources.Descriptor
	schema    json.RawMessage
	example   json.RawMessage
}

var _ adapter.ResourceAdapter = (*datasourceAdapter)(nil)

func (a *datasourceAdapter) Descriptor() resources.Descriptor { return a.desc }
func (a *datasourceAdapter) Aliases() []string                { return nil }
func (a *datasourceAdapter) Schema() json.RawMessage          { return a.schema }
func (a *datasourceAdapter) Example() json.RawMessage         { return a.example }

func (a *datasourceAdapter) List(ctx context.Context, opts metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	items, err := a.client.List(ctx)
	if err != nil {
		return nil, err
	}
	if opts.Limit > 0 && int64(len(items)) > opts.Limit {
		items = items[:opts.Limit]
	}
	out := &unstructured.UnstructuredList{}
	for _, ds := range items {
		u, err := datasourceToUnstructured(ds, a.namespace)
		if err != nil {
			return nil, err
		}
		out.Items = append(out.Items, *u)
	}
	return out, nil
}

func (a *datasourceAdapter) Get(ctx context.Context, name string, _ metav1.GetOptions) (*unstructured.Unstructured, error) {
	ds, err := a.client.GetByUID(ctx, name)
	if err != nil {
		if dsclient.IsNotFound(err) {
			return nil, apierrors.NewNotFound(
				schema.GroupResource{Group: a.desc.GroupVersion.Group, Resource: a.desc.Plural}, name)
		}
		return nil, err
	}
	return datasourceToUnstructured(ds, a.namespace)
}

func (a *datasourceAdapter) Create(ctx context.Context, obj *unstructured.Unstructured, opts metav1.CreateOptions) (*unstructured.Unstructured, error) {
	ds, err := unstructuredToDatasource(obj)
	if err != nil {
		return nil, err
	}
	dsclient.WarnIfSecretMissing(ds)
	if isDryRun(opts.DryRun) {
		return obj, nil
	}
	created, err := a.client.Create(ctx, ds)
	if err != nil {
		return nil, fmt.Errorf("failed to create datasource: %w", err)
	}
	return datasourceToUnstructured(created, a.namespace)
}

func (a *datasourceAdapter) Update(ctx context.Context, obj *unstructured.Unstructured, opts metav1.UpdateOptions) (*unstructured.Unstructured, error) {
	ds, err := unstructuredToDatasource(obj)
	if err != nil {
		return nil, err
	}
	dsclient.WarnIfSecretMissing(ds)
	name := obj.GetName()
	if isDryRun(opts.DryRun) {
		return obj, nil
	}
	updated, err := a.client.Update(ctx, name, ds)
	if err != nil {
		return nil, fmt.Errorf("failed to update datasource %q: %w", name, err)
	}
	return datasourceToUnstructured(updated, a.namespace)
}

func (a *datasourceAdapter) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	if isDryRun(opts.DryRun) {
		return nil
	}
	return a.client.Delete(ctx, name)
}

func isDryRun(dryRun []string) bool {
	return slices.Contains(dryRun, metav1.DryRunAll)
}

// datasourceToUnstructured renders a wire datasource as the converged manifest
// envelope: per-plugin apiVersion, top-level `secure` placeholders, and a
// `spec` keyed by `title`. Secret values are never present on reads.
func datasourceToUnstructured(ds *dsclient.Datasource, namespace string) (*unstructured.Unstructured, error) {
	m := dsclient.ManifestFromDatasource(ds)

	b, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("marshal datasource manifest: %w", err)
	}
	var obj map[string]any
	if err := json.Unmarshal(b, &obj); err != nil {
		return nil, fmt.Errorf("unmarshal datasource manifest: %w", err)
	}

	if namespace != "" {
		meta, ok := obj["metadata"].(map[string]any)
		if !ok {
			meta = map[string]any{}
			obj["metadata"] = meta
		}
		meta["namespace"] = namespace
	}
	return &unstructured.Unstructured{Object: obj}, nil
}

// unstructuredToDatasource maps a converged manifest envelope into the wire
// datasource, resolving the top-level `secure` block (including fromEnv /
// fromFile indirection) into secureJsonData before send. The datasource type is
// read from spec.type, which is required on the resources path because the group
// is normalized to the canonical descriptor before routing.
func unstructuredToDatasource(obj *unstructured.Unstructured) (*dsclient.Datasource, error) {
	b, err := json.Marshal(obj.Object)
	if err != nil {
		return nil, fmt.Errorf("marshal object: %w", err)
	}
	var m dsclient.DataSourceManifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("unmarshal datasource manifest: %w", err)
	}
	if err := m.ResolveSecrets(""); err != nil {
		return nil, err
	}
	ds := m.ToDatasource()
	if ds.Type == "" {
		return nil, errors.New("spec.type is required (the datasource plugin id)")
	}
	return ds, nil
}
