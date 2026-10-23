package pluginsdk_test

import (
	"context"
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
	if os.Getenv("GCX_PLUGIN") == "generic-commands-v1" {
		if os.Getenv("GCX_TEST_PROTOCOL") == "99" {
			plugin.Serve(&plugin.ServeConfig{HandshakeConfig: plugin.HandshakeConfig{ProtocolVersion: 99, MagicCookieKey: "GCX_PLUGIN", MagicCookieValue: "generic-commands-v1"}, Plugins: map[string]plugin.Plugin{"extension": &incompatiblePlugin{}}, GRPCServer: plugin.DefaultGRPCServer})
			return
		}
		sdk.Serve(testApp{})
		return
	}
	os.Exit(m.Run())
}

type testApp struct{}

func (testApp) Run(ctx context.Context, r *sdk.RunRequest, host sdk.Executor) (*sdk.RunResponse, error) {
	if len(r.GetArgs()) > 0 && r.GetArgs()[0] == "crash" {
		os.Exit(9)
	}
	if len(r.GetArgs()) > 0 && r.GetArgs()[0] == "exit" {
		return &sdk.RunResponse{ExitCode: 7, Stdout: []byte("raw result"), Stderr: "diagnostic"}, nil
	}
	if len(r.GetArgs()) > 0 && r.GetArgs()[0] == "cancel-call" {
		callCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		_, err := host.Execute(callCtx, sdk.CommandRequest{Args: []string{"wait"}})
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			return nil, errors.New("individual call was not cancelled")
		}
		result, err := host.Execute(ctx, sdk.CommandRequest{Args: []string{"after-cancel"}})
		return &sdk.RunResponse{Stdout: result.Stdout}, err
	}
	result, err := host.Execute(ctx, sdk.CommandRequest{Args: r.GetArgs(), Stdin: []byte("request input")})
	if err != nil {
		return nil, err
	}
	return &sdk.RunResponse{Stdout: result.Stdout, Stderr: string(result.Stderr), ExitCode: int32(result.ExitCode)}, nil //nolint:gosec // The SDK result originated from an int32 protobuf exit code.
}

type testHost struct {
	sdk.UnimplementedCommandHostServer

	entered   chan struct{}
	cancelled chan struct{}
	deny      bool
	args      []string
	stdin     string
}

func (h *testHost) Execute(ctx context.Context, r *sdk.ExecuteRequest) (*sdk.ExecuteResponse, error) {
	if h.deny {
		return nil, status.Error(codes.PermissionDenied, "access denied")
	}
	if len(r.GetArgs()) > 0 && r.GetArgs()[0] == "wait" {
		<-ctx.Done()
		return nil, status.FromContextError(ctx.Err()).Err()
	}
	h.args, h.stdin = r.GetArgs(), string(r.GetStdin())
	if h.entered != nil {
		close(h.entered)
		<-ctx.Done()
		close(h.cancelled)
		return nil, status.FromContextError(ctx.Err()).Err()
	}
	return &sdk.ExecuteResponse{Stdout: []byte(`{"generation":9007199254740993}`), Stderr: []byte("partial failure"), ExitCode: 4}, nil
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
		{name: "callback", args: []string{"resources", "get", "things"}, code: 4},
		{name: "individual cancellation", args: []string{"cancel-call"}},
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
			if tc.name == "callback" && (len(host.args) != 3 || host.args[2] != "things" || host.stdin != "request input" || string(result.GetStdout()) != `{"generation":9007199254740993}` || result.GetStderr() != "partial failure") {
				t.Fatalf("bad roundtrip: %v", result)
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
