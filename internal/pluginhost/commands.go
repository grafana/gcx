//go:build !wasip1

// Package pluginhost executes gcx commands requested by a plugin.
package pluginhost

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"

	sdk "github.com/grafana/gcx/experimental/pluginsdk"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Commands struct {
	sdk.UnimplementedCommandHostServer

	Binary     string
	Prefix     []string
	Context    string
	ConfigFile string
}

func (h *Commands) Execute(ctx context.Context, request *sdk.ExecuteRequest) (*sdk.ExecuteResponse, error) {
	if len(request.GetArgs()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "gcx command arguments are required")
	}
	args := append(append([]string(nil), h.Prefix...), request.GetArgs()...)
	cmd := exec.CommandContext(ctx, h.Binary, args...) //nolint:gosec // The host selects the executable; plugin arguments never pass through a shell.
	cmd.Env = append(os.Environ(), "GCX_CONTEXT="+h.Context)
	if h.ConfigFile != "" {
		cmd.Env = append(cmd.Env, "GCX_CONFIG="+h.ConfigFile)
	}
	cmd.Stdin = bytes.NewReader(request.GetStdin())
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	result := &sdk.ExecuteResponse{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	if ctx.Err() != nil {
		return nil, status.FromContextError(ctx.Err()).Err()
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		result.ExitCode = int32(exit.ExitCode()) //nolint:gosec // OS process exit codes fit in int32.
		return result, nil
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "execute gcx: %v", err)
	}
	return result, nil
}
