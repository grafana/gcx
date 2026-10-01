package host

import (
	"context"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
)

// Command mirrors [exec.CommandContext].
func Command(ctx context.Context, name string, args ...string) (*exec.Cmd, error) {
	if Sandboxed(ctx) {
		return nil, opErr("exec " + name)
	}
	return exec.CommandContext(ctx, name, args...), nil
}

// LookPath mirrors [exec.LookPath].
func LookPath(ctx context.Context, file string) (string, error) {
	if Sandboxed(ctx) {
		return "", opErr("exec " + file)
	}
	return exec.LookPath(file)
}

// Listen mirrors [net.ListenConfig.Listen].
func Listen(ctx context.Context, network, address string) (net.Listener, error) {
	if Sandboxed(ctx) {
		return nil, opErr("listen " + address)
	}
	var lc net.ListenConfig
	return lc.Listen(ctx, network, address)
}

// NotifyContext mirrors [signal.NotifyContext]. Inside a sandbox signals
// belong to the embedding process, so the returned context is only cancelled
// by stop or by ctx.
func NotifyContext(ctx context.Context, signals ...os.Signal) (context.Context, context.CancelFunc) {
	if Sandboxed(ctx) {
		return context.WithCancel(ctx)
	}
	return signal.NotifyContext(ctx, signals...)
}

// CurrentUser mirrors [user.Current].
func CurrentUser(ctx context.Context) (*user.User, error) {
	if Sandboxed(ctx) {
		return nil, opErr("current user")
	}
	return user.Current()
}
