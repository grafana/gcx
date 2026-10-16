package pluginsdk_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	sdk "github.com/grafana/gcx/experimental/pluginsdk"
	"github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestMain(m *testing.M) {
	if os.Getenv("GCX_PLUGIN") == "typed-resources-v1" {
		if os.Getenv("GCX_TEST_PROTOCOL") == "99" {
			plugin.Serve(&plugin.ServeConfig{HandshakeConfig: plugin.HandshakeConfig{ProtocolVersion: 99, MagicCookieKey: "GCX_PLUGIN", MagicCookieValue: "typed-resources-v1"}, Plugins: map[string]plugin.Plugin{"extension": &incompatiblePlugin{}}, GRPCServer: plugin.DefaultGRPCServer})
			return
		}
		sdk.Serve(testApp{})
		return
	}
	os.Exit(m.Run())
}

type testApp struct{}

func (testApp) Run(ctx context.Context, r *sdk.RunRequest, host sdk.ResourceHostClient) (*sdk.RunResponse, error) {
	if len(r.GetArgs()) > 0 && r.GetArgs()[0] == "crash" {
		os.Exit(9)
	}
	if len(r.GetArgs()) > 0 && r.GetArgs()[0] == "exit" {
		return &sdk.RunResponse{ExitCode: 7, Stdout: []byte("raw result"), Stderr: "diagnostic"}, nil
	}
	types, err := host.ListTypes(ctx, &sdk.ListTypesRequest{Selectors: r.GetArgs()})
	if err != nil {
		return nil, err
	}
	if _, err := host.GetSchemas(ctx, &sdk.GetSchemasRequest{Types: types.GetTypes()}); err != nil {
		return nil, err
	}
	result, err := host.ListResources(ctx, &sdk.ListResourcesRequest{Type: types.GetTypes()[0]})
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(result)
	return &sdk.RunResponse{Stdout: data}, err
}

type testHost struct {
	sdk.UnimplementedResourceHostServer

	entered   chan struct{}
	cancelled chan struct{}
	deny      bool
	selectors []string
}

func (h *testHost) ListTypes(_ context.Context, r *sdk.ListTypesRequest) (*sdk.ListTypesResponse, error) {
	h.selectors = r.GetSelectors()
	if h.deny {
		return nil, status.Error(codes.PermissionDenied, "access denied")
	}
	return &sdk.ListTypesResponse{Types: []*sdk.ResourceType{{Group: "test.app", Version: "v1", Kind: "Thing", Plural: "things"}}}, nil
}
func (*testHost) GetSchemas(_ context.Context, r *sdk.GetSchemasRequest) (*sdk.GetSchemasResponse, error) {
	if len(r.GetTypes()) != 1 || r.GetTypes()[0].GetGroup() != "test.app" {
		return nil, errors.New("wrong schema type")
	}
	return &sdk.GetSchemasResponse{}, nil
}
func (h *testHost) ListResources(ctx context.Context, r *sdk.ListResourcesRequest) (*sdk.ListResourcesResponse, error) {
	if h.entered != nil {
		close(h.entered)
		<-ctx.Done()
		close(h.cancelled)
		return nil, ctx.Err()
	}
	if r.GetType().GetPlural() != "things" {
		return nil, errors.New("wrong list type")
	}
	return &sdk.ListResourcesResponse{Items: []*sdk.Resource{{Metadata: &sdk.ResourceMetadata{Name: "one"}, ObjectJson: []byte(`{"metadata":{"generation":9007199254740993}}`)}}, Failures: []*sdk.ResourceFailure{{Message: "second page failed"}}}, nil
}
func TestRun(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		args    []string
		deny    bool
		code    int32
		wantErr bool
	}{
		{name: "callbacks", args: []string{"things"}},
		{name: "application exit", args: []string{"exit"}, code: 7},
		{name: "crash", args: []string{"crash"}, wantErr: true},
		{name: "permission denied", deny: true, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			host := &testHost{deny: tc.deny}
			result, err := sdk.Run(t.Context(), sdk.Options{Binary: binary, Args: tc.args}, host)
			if (err != nil) != tc.wantErr {
				t.Fatalf("result=%v error=%v", result, err)
			}
			if tc.deny && status.Code(err) != codes.PermissionDenied {
				t.Fatalf("lost status: %v", err)
			}
			if err != nil {
				return
			}
			if result.GetExitCode() != tc.code {
				t.Fatalf("exit=%d", result.GetExitCode())
			}
			if tc.name == "callbacks" {
				var got sdk.ListResourcesResponse
				if err := json.Unmarshal(result.GetStdout(), &got); err != nil {
					t.Fatal(err)
				}
				if len(host.selectors) != 1 || host.selectors[0] != "things" || got.GetItems()[0].GetMetadata().GetName() != "one" || len(got.GetFailures()) != 1 || string(got.GetItems()[0].GetObjectJson()) != `{"metadata":{"generation":9007199254740993}}` {
					t.Fatalf("bad roundtrip: %s", result.GetStdout())
				}
			}
		})
	}
}
func TestCancellationReachesHostCallback(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	host := &testHost{entered: make(chan struct{}), cancelled: make(chan struct{})}
	done := make(chan error, 1)
	go func() { _, err := sdk.Run(ctx, sdk.Options{Binary: binary}, host); done <- err }()
	select {
	case <-host.entered:
	case <-time.After(15 * time.Second):
		t.Fatal("callback did not start")
	}
	cancel()
	select {
	case <-host.cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("callback was not cancelled")
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("plugin did not stop")
	}
}

// This fixture advertises an incompatible protocol before any app can run.
type incompatiblePlugin struct{ plugin.NetRPCUnsupportedPlugin }

func (*incompatiblePlugin) GRPCServer(*plugin.GRPCBroker, *grpc.Server) error { return nil }
func (*incompatiblePlugin) GRPCClient(context.Context, *plugin.GRPCBroker, *grpc.ClientConn) (any, error) {
	return nil, errors.New("incompatible fixture must not be dispensed")
}
func TestProtocolMismatch(t *testing.T) {
	t.Setenv("GCX_TEST_PROTOCOL", "99")
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sdk.Run(t.Context(), sdk.Options{Binary: binary}, &testHost{}); err == nil {
		t.Fatal("incompatible protocol accepted")
	}
}
