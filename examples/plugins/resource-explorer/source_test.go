package main

import (
	"context"
	"errors"
	"testing"

	"github.com/grafana/gcx-plugin-resource-explorer/browse"
	sdk "github.com/grafana/gcx/experimental/pluginsdk"
	"google.golang.org/grpc"
)

type fakeHost struct {
	sdk.ResourceHostClient

	requested *sdk.ResourceType
}

func (*fakeHost) ListTypes(context.Context, *sdk.ListTypesRequest, ...grpc.CallOption) (*sdk.ListTypesResponse, error) {
	return &sdk.ListTypesResponse{Types: []*sdk.ResourceType{{Group: "test.app", Version: "v1", Kind: "First", Plural: "firsts"}, {Group: "test.app", Version: "v1", Kind: "Second", Plural: "seconds"}}}, nil
}
func (*fakeHost) GetSchemas(_ context.Context, r *sdk.GetSchemasRequest, _ ...grpc.CallOption) (*sdk.GetSchemasResponse, error) {
	response := &sdk.GetSchemasResponse{}
	for _, typ := range r.GetTypes() {
		response.Schemas = append(response.Schemas, &sdk.ResourceSchema{Type: typ, SpecJson: []byte(`{"type":"object"}`)})
	}
	return response, nil
}
func (h *fakeHost) ListResources(_ context.Context, r *sdk.ListResourcesRequest, _ ...grpc.CallOption) (*sdk.ListResourcesResponse, error) {
	h.requested = r.GetType()
	return &sdk.ListResourcesResponse{Items: []*sdk.Resource{{ObjectJson: []byte(`{"metadata":{"name":"no-kind","generation":9007199254740993}}`)}}, Failures: []*sdk.ResourceFailure{{Message: "page unavailable"}}}, nil
}
func TestTypedSource(t *testing.T) {
	host := &fakeHost{}
	src := &rpcSource{host: host}
	types, err := src.Types(t.Context())
	if err != nil || len(types) != 2 {
		t.Fatalf("types=%v err=%v", types, err)
	}
	schemas, err := src.Schemas(t.Context(), types)
	if err != nil || len(schemas) != 2 {
		t.Fatalf("schemas=%v err=%v", schemas, err)
	}
	for _, d := range types {
		objects, err := src.List(t.Context(), d)
		var partial *browse.PartialError
		if !errors.As(err, &partial) || partial.Failed != 1 || len(objects) != 1 {
			t.Fatalf("items=%v err=%v", objects, err)
		}
		if host.requested.GetKind() != d.Kind || host.requested.GetPlural() != d.Plural || host.requested.GetGroup() != d.GroupVersion.Group {
			t.Fatal("type mapping lost")
		}
		if objects[0].GetKind() != "" || objects[0].GetGeneration() != 9007199254740993 {
			t.Fatal(objects[0])
		}
	}
}
