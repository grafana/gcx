//go:build !wasip1

package pluginhost

import (
	"context"
	"encoding/json"
	"strings"

	sdk "github.com/grafana/gcx/experimental/pluginsdk"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Demo struct {
	sdk.UnimplementedResourceHostServer
}

func demoTypes() []*sdk.ResourceType {
	return []*sdk.ResourceType{{Group: "demo.grafana.app", Version: "v1", Kind: "Dashboard", Plural: "dashboards", Singular: "dashboard"}, {Group: "demo.grafana.app", Version: "v1", Kind: "Folder", Plural: "folders", Singular: "folder"}}
}
func (*Demo) ListTypes(_ context.Context, req *sdk.ListTypesRequest) (*sdk.ListTypesResponse, error) {
	out := &sdk.ListTypesResponse{Types: []*sdk.ResourceType{}}
	for _, d := range demoTypes() {
		match := len(req.GetSelectors()) == 0
		for _, s := range req.GetSelectors() {
			if s == d.GetPlural() || s == d.GetSingular() || s == d.GetPlural()+"."+d.GetVersion()+"."+d.GetGroup() {
				match = true
			}
		}
		if match {
			out.Types = append(out.Types, d)
		}
	}
	if len(out.GetTypes()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "unknown demo selector")
	}
	return out, nil
}
func (*Demo) GetSchemas(_ context.Context, req *sdk.GetSchemasRequest) (*sdk.GetSchemasResponse, error) {
	out := &sdk.GetSchemasResponse{}
	for _, d := range req.GetTypes() {
		out.Schemas = append(out.Schemas, &sdk.ResourceSchema{Type: d, SpecJson: []byte(`{"type":"object","properties":{"title":{"type":"string"}}}`)})
	}
	return out, nil
}
func (*Demo) ListResources(_ context.Context, req *sdk.ListResourcesRequest) (*sdk.ListResourcesResponse, error) {
	if req.GetType() == nil {
		return nil, status.Error(codes.InvalidArgument, "resource type is required")
	}
	d := req.GetType()
	out := &sdk.ListResourcesResponse{}
	for _, suffix := range []string{"", "-without-kind"} {
		name := "demo-" + strings.ToLower(d.GetKind()) + suffix
		object := map[string]any{"apiVersion": d.GetGroup() + "/" + d.GetVersion(), "metadata": map[string]any{"name": name, "generation": int64(9007199254740993)}, "spec": map[string]any{"title": "Offline " + d.GetKind()}}
		kind := ""
		if suffix == "" {
			object["kind"] = d.GetKind()
			kind = d.GetKind()
		}
		raw, err := json.Marshal(object)
		if err != nil {
			return nil, err
		}
		out.Items = append(out.Items, &sdk.Resource{Metadata: &sdk.ResourceMetadata{Name: name}, ApiVersion: d.GetGroup() + "/" + d.GetVersion(), Kind: kind, ObjectJson: raw})
	}
	return out, nil
}
