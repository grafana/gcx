package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/grafana/gcx-plugin-resource-explorer/browse"
	"github.com/grafana/gcx-plugin-resource-explorer/resources"
	sdk "github.com/grafana/gcx/experimental/pluginsdk"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kjson "k8s.io/apimachinery/pkg/util/json"
)

type rpcSource struct {
	host      sdk.ResourceHostClient
	selectors []string
}

func typed(d resources.Descriptor) *sdk.ResourceType {
	return &sdk.ResourceType{Group: d.GroupVersion.Group, Version: d.GroupVersion.Version, Kind: d.Kind, Plural: d.Plural, Singular: d.Singular}
}
func (s *rpcSource) Types(ctx context.Context) (resources.Descriptors, error) {
	response, err := s.host.ListTypes(ctx, &sdk.ListTypesRequest{Selectors: s.selectors})
	if err != nil {
		return nil, err
	}
	out := make(resources.Descriptors, 0, len(response.GetTypes()))
	for _, t := range response.GetTypes() {
		out = append(out, resources.Descriptor{GroupVersion: schema.GroupVersion{Group: t.GetGroup(), Version: t.GetVersion()}, Kind: t.GetKind(), Plural: t.GetPlural(), Singular: t.GetSingular()})
	}
	return out.Sorted(), nil
}
func (s *rpcSource) Schemas(ctx context.Context, descs resources.Descriptors) (map[string]map[string]any, error) {
	req := &sdk.GetSchemasRequest{}
	for _, d := range descs {
		req.Types = append(req.Types, typed(d))
	}
	response, err := s.host.GetSchemas(ctx, req)
	if err != nil {
		return nil, err
	}
	out := map[string]map[string]any{}
	for _, item := range response.GetSchemas() {
		var spec map[string]any
		if err := json.Unmarshal(item.GetSpecJson(), &spec); err != nil {
			return nil, err
		}
		key := item.GetType().GetPlural() + "." + item.GetType().GetVersion() + "." + item.GetType().GetGroup()
		out[key] = spec
	}
	if len(response.GetWarnings()) > 0 {
		return out, errors.New(strings.Join(response.GetWarnings(), "; "))
	}
	return out, nil
}
func (s *rpcSource) List(ctx context.Context, d resources.Descriptor) ([]unstructured.Unstructured, error) {
	response, err := s.host.ListResources(ctx, &sdk.ListResourcesRequest{Type: typed(d)})
	if err != nil {
		return nil, err
	}
	items := make([]unstructured.Unstructured, 0, len(response.GetItems()))
	for _, item := range response.GetItems() {
		var obj map[string]any
		// Resource bodies can omit kind; preserve the original object and integer metadata.
		if err := kjson.Unmarshal(item.GetObjectJson(), &obj); err != nil {
			return nil, err
		}
		items = append(items, unstructured.Unstructured{Object: obj})
	}
	warnings := append([]string(nil), response.GetWarnings()...)
	for _, failure := range response.GetFailures() {
		warnings = append(warnings, failure.GetMessage())
	}
	if response.GetTruncated() {
		warnings = append(warnings, "resource list is truncated")
	}
	if len(warnings) > 0 {
		return items, &browse.PartialError{Failed: len(response.GetFailures()), Err: errors.New(strings.Join(warnings, "; "))}
	}
	return items, nil
}
