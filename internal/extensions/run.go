package extensions

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/extensions/extensionpb"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// The handshake values must match those in the extsdk module.
const (
	protocolVersion  = 1
	magicCookieKey   = "GCX_EXTENSION"
	magicCookieValue = "gcx-extension"
	pluginName       = "extension"
	// maxStdinSize bounds an ExecuteRequest, which carries the command's stdin.
	maxStdinSize = 64 << 20
)

// RunOptions configures one run of an extension.
type RunOptions struct {
	// Args are passed to the extension unchanged.
	Args []string
	// GCX is the gcx executable that runs the extension's commands.
	GCX string
	// Context and ConfigFile are passed to every command the extension runs,
	// so they all use the same stack. Empty values are not passed.
	Context    string
	ConfigFile string
	// Stdout and Stderr receive the extension's own output.
	Stdout io.Writer
	Stderr io.Writer
}

// Run starts the extension, serves its gcx commands until it returns, and
// returns its exit code.
func (e *Extension) Run(ctx context.Context, opts RunOptions) (int, error) {
	// Output arrives through Host.Output and, outside Run, go-plugin's stdio
	// forwarding, from different goroutines.
	var mu sync.Mutex
	stdout := &lockedWriter{mu: &mu, w: opts.Stdout}
	stderr := &lockedWriter{mu: &mu, w: opts.Stderr}
	client := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig: plugin.HandshakeConfig{
			ProtocolVersion:  protocolVersion,
			MagicCookieKey:   magicCookieKey,
			MagicCookieValue: magicCookieValue,
		},
		Plugins:          map[string]plugin.Plugin{pluginName: &extensionPlugin{}},
		Cmd:              exec.Command(e.Executable), //nolint:gosec,noctx // go-plugin owns the process lifecycle
		AllowedProtocols: []plugin.Protocol{plugin.ProtocolGRPC},
		AutoMTLS:         true,
		StartTimeout:     10 * time.Second,
		Logger:           hclog.NewNullLogger(),
		Stderr:           stderr,
		SyncStdout:       stdout,
		SyncStderr:       stderr,
	})
	defer client.Kill()

	rpc, err := client.Client()
	if err != nil {
		return 0, fmt.Errorf("start extension %s: %w", e.Name, err)
	}
	raw, err := rpc.Dispense(pluginName)
	if err != nil {
		return 0, fmt.Errorf("start extension %s: %w", e.Name, err)
	}
	ext, ok := raw.(*extensionClient)
	if !ok {
		return 0, fmt.Errorf("start extension %s: unexpected client %T", e.Name, raw)
	}

	host := &hostServer{opts: opts, stdout: stdout, stderr: stderr}
	brokerID := ext.broker.NextId()
	served := make(chan *grpc.Server, 1)
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		ext.broker.AcceptAndServe(brokerID, func(serverOpts []grpc.ServerOption) *grpc.Server {
			s := grpc.NewServer(append(serverOpts, grpc.MaxRecvMsgSize(maxStdinSize))...)
			extensionpb.RegisterHostServer(s, host)
			served <- s
			return s
		})
	}()
	select {
	case s := <-served:
		defer s.Stop()
	case <-stopped:
		return 0, fmt.Errorf("start extension %s: could not serve gcx commands", e.Name)
	case <-ctx.Done():
		return 0, ctx.Err()
	}

	resp, err := ext.client.Run(ctx, &extensionpb.RunRequest{HostBrokerId: brokerID, Args: opts.Args, AgentMode: agent.IsAgentMode()})
	if err != nil {
		return 0, contextError(ctx, fmt.Errorf("run extension %s: %w", e.Name, err))
	}
	return int(resp.GetExitCode()), nil
}

// contextError returns ctx's error in place of the RPC error it caused. gRPC
// can report an expired deadline before ctx itself is done.
func contextError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if status.Code(err) == codes.DeadlineExceeded {
		return context.DeadlineExceeded
	}
	return err
}

type extensionPlugin struct {
	plugin.NetRPCUnsupportedPlugin
}

func (extensionPlugin) GRPCServer(*plugin.GRPCBroker, *grpc.Server) error {
	return errors.New("gcx does not serve extensions")
}

func (extensionPlugin) GRPCClient(_ context.Context, broker *plugin.GRPCBroker, conn *grpc.ClientConn) (any, error) {
	return &extensionClient{broker: broker, client: extensionpb.NewExtensionClient(conn)}, nil
}

type extensionClient struct {
	broker *plugin.GRPCBroker
	client extensionpb.ExtensionClient
}

type lockedWriter struct {
	mu *sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// hostServer runs each requested command in a new gcx process.
type hostServer struct {
	extensionpb.UnimplementedHostServer

	opts           RunOptions
	stdout, stderr io.Writer
}

func (h *hostServer) Output(stream extensionpb.Host_OutputServer) error {
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return stream.SendAndClose(&extensionpb.OutputResponse{})
		}
		if err != nil {
			return err
		}
		w := h.stdout
		if chunk.GetStream() == extensionpb.OutputChunk_STREAM_STDERR {
			w = h.stderr
		}
		if _, err := w.Write(chunk.GetData()); err != nil {
			return status.Errorf(codes.Internal, "write output: %v", err)
		}
	}
}

func (h *hostServer) Execute(req *extensionpb.ExecuteRequest, stream extensionpb.Host_ExecuteServer) error {
	if len(req.GetArgs()) == 0 {
		return status.Error(codes.InvalidArgument, "gcx arguments are required")
	}
	ctx := stream.Context()
	cmd := exec.CommandContext(ctx, h.opts.GCX, req.GetArgs()...) //nolint:gosec // arguments are passed without a shell
	// --agent is read from gcx's own arguments, so pass the resolved mode on.
	cmd.Env = append(os.Environ(), "GCX_AGENT_MODE="+strconv.FormatBool(agent.IsAgentMode()))
	if h.opts.Context != "" {
		cmd.Env = append(cmd.Env, config.ContextEnvVar+"="+h.opts.Context)
	}
	if h.opts.ConfigFile != "" {
		cmd.Env = append(cmd.Env, config.ConfigFileEnvVar+"="+h.opts.ConfigFile)
	}
	cmd.Stdin = bytes.NewReader(req.GetStdin())
	// exec copies each pipe from its own goroutine, in chunks of at most 32 KiB.
	var mu sync.Mutex
	cmd.Stdout = &chunkSender{mu: &mu, stream: stream, kind: extensionpb.OutputChunk_STREAM_STDOUT}
	cmd.Stderr = &chunkSender{mu: &mu, stream: stream, kind: extensionpb.OutputChunk_STREAM_STDERR}

	err := cmd.Run()
	if ctx.Err() != nil {
		return status.FromContextError(ctx.Err()).Err()
	}
	code := 0
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		code = exitErr.ExitCode()
	} else if err != nil {
		return status.Errorf(codes.Internal, "run gcx: %v", err)
	}
	return stream.Send(&extensionpb.ExecuteEvent{Event: &extensionpb.ExecuteEvent_ExitCode{ExitCode: int32(code)}})
}

// chunkSender sends a command's output to the extension as it is written.
type chunkSender struct {
	mu     *sync.Mutex // gRPC streams do not support concurrent sends
	stream extensionpb.Host_ExecuteServer
	kind   extensionpb.OutputChunk_Stream
}

func (c *chunkSender) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	chunk := &extensionpb.OutputChunk{Stream: c.kind, Data: p}
	if err := c.stream.Send(&extensionpb.ExecuteEvent{Event: &extensionpb.ExecuteEvent_Output{Output: chunk}}); err != nil {
		return 0, err
	}
	return len(p), nil
}
