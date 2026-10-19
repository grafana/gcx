//go:build !wasip1

// Package pluginhost implements the PoC's typed, read-only host capabilities.
package pluginhost

import (
	"context"
	"encoding/json"
	"fmt"

	sdk "github.com/grafana/gcx/experimental/pluginsdk"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/deeplink"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/resources/discovery"
	"github.com/grafana/gcx/internal/resources/remote"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Resources struct {
	sdk.UnimplementedResourceHostServer

	ctx        context.Context //nolint:containedctx // RPC contexts lack the configuration values owned by this host session.
	registry   *discovery.Registry
	puller     *remote.Puller
	schemas    *discovery.SchemaFetcher
	grafanaURL string
}

func New(ctx context.Context, cfg config.NamespacedRESTConfig) (*Resources, error) {
	registry, err := discovery.NewDefaultRegistry(ctx, cfg)
	if err != nil {
		return nil, err
	}
	puller, err := remote.NewDefaultPullerWithRegistry(cfg, registry)
	if err != nil {
		return nil, err
	}
	schemas, err := discovery.NewSchemaFetcher(&cfg.Config)
	if err != nil {
		return nil, err
	}
	return &Resources{ctx: ctx, registry: registry, puller: puller, schemas: schemas, grafanaURL: cfg.GrafanaURL}, nil
}

// Preserve host configuration context values while propagating RPC cancellation.
func (h *Resources) requestContext(rpc context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancel(h.ctx)
	stop := context.AfterFunc(rpc, cancel)
	if rpc.Err() != nil {
		cancel()
	}
	return ctx, func() { stop(); cancel() }
}

func (h *Resources) ListTypes(ctx context.Context, req *sdk.ListTypesRequest) (*sdk.ListTypesResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, status.FromContextError(err).Err()
	}
	descs := h.registry.SupportedResources()
	if len(req.GetSelectors()) > 0 {
		selectors, err := resources.ParseSelectors(req.GetSelectors())
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		filters, err := h.registry.MakeFilters(discovery.MakeFiltersOptions{Selectors: selectors, PreferredVersionOnly: true})
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		descs = nil
		seen := map[string]bool{}
		for _, f := range filters {
			if !seen[f.Descriptor.String()] {
				descs = append(descs, f.Descriptor)
				seen[f.Descriptor.String()] = true
			}
		}
	}
	out := &sdk.ListTypesResponse{Types: make([]*sdk.ResourceType, 0, len(descs))}
	for _, d := range descs.Sorted() {
		out.Types = append(out.Types, resourceType(d))
	}
	return out, nil
}
func resourceType(d resources.Descriptor) *sdk.ResourceType {
	return &sdk.ResourceType{Group: d.GroupVersion.Group, Version: d.GroupVersion.Version, Kind: d.Kind, Plural: d.Plural, Singular: d.Singular}
}
func (h *Resources) descriptor(t *sdk.ResourceType) (resources.Descriptor, error) {
	if t != nil {
		for _, d := range h.registry.SupportedResources() {
			if d.GroupVersion.Group == t.GetGroup() && d.GroupVersion.Version == t.GetVersion() && d.Plural == t.GetPlural() && d.Kind == t.GetKind() {
				return d, nil
			}
		}
	}
	return resources.Descriptor{}, status.Error(codes.InvalidArgument, "resource type must match a discovered group, version, plural and kind")
}
func (h *Resources) GetSchemas(rpc context.Context, req *sdk.GetSchemasRequest) (*sdk.GetSchemasResponse, error) {
	ctx, cancel := h.requestContext(rpc)
	defer cancel()
	descs := make(resources.Descriptors, 0, len(req.GetTypes()))
	for _, t := range req.GetTypes() {
		d, err := h.descriptor(t)
		if err != nil {
			return nil, err
		}
		descs = append(descs, d)
	}
	schemas, fetchErr := h.schemas.FetchSpecSchemas(ctx, descs)
	if ctx.Err() != nil {
		return nil, status.FromContextError(ctx.Err()).Err()
	}
	out := &sdk.GetSchemasResponse{}
	if fetchErr != nil {
		out.Warnings = append(out.Warnings, fetchErr.Error())
	}
	for _, d := range descs {
		raw := adapter.SchemaForGVK(d.GroupVersionKind())
		key := d.GroupVersion.Group + "/" + d.GroupVersion.Version + "/" + d.Kind
		if s, ok := schemas[key]; ok {
			var err error
			raw, err = json.Marshal(s)
			if err != nil {
				return nil, err
			}
		}
		if len(raw) > 0 {
			out.Schemas = append(out.Schemas, &sdk.ResourceSchema{Type: resourceType(d), SpecJson: raw})
		}
	}
	return out, nil
}
func (h *Resources) ListResources(rpc context.Context, req *sdk.ListResourcesRequest) (*sdk.ListResourcesResponse, error) {
	d, err := h.descriptor(req.GetType())
	if err != nil {
		return nil, err
	}
	ctx, cancel := h.requestContext(rpc)
	defer cancel()
	var result resources.Resources
	summary, err := h.puller.Pull(ctx, remote.PullRequest{Filters: resources.Filters{{Type: resources.FilterTypeAll, Descriptor: d}}, Resources: &result, Limit: 0})
	if ctx.Err() != nil {
		return nil, status.FromContextError(ctx.Err()).Err()
	}
	if err != nil {
		return nil, err
	}
	objects := result.ToUnstructuredList().Items
	resources.SortUnstructured(objects)
	deeplink.InjectURLs(objects, h.grafanaURL)
	out := &sdk.ListResourcesResponse{Items: make([]*sdk.Resource, 0, len(objects)), Truncated: summary.IsTruncated(), Warnings: summary.Warnings()}
	for reason, count := range summary.SkippedReasons() {
		out.Warnings = append(out.Warnings, fmt.Sprintf("%d skipped: %s", count, reason))
	}
	for _, failure := range summary.Failures() {
		out.Failures = append(out.Failures, &sdk.ResourceFailure{Message: fmt.Sprint(failure.Error)})
	}
	for _, obj := range objects {
		data, err := json.Marshal(obj.Object)
		if err != nil {
			return nil, err
		}
		out.Items = append(out.Items, &sdk.Resource{Metadata: &sdk.ResourceMetadata{Name: obj.GetName(), Namespace: obj.GetNamespace(), Uid: string(obj.GetUID())}, ApiVersion: obj.GetAPIVersion(), Kind: obj.GetKind(), ObjectJson: data})
	}
	return out, nil
}
