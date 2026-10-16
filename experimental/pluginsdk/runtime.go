// Package pluginsdk defines the typed, versioned RPC contract for the plugin PoC.
package pluginsdk

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
)

const ProtocolVersion = 1
const maxMessageSize = 64 << 20

func handshake() plugin.HandshakeConfig {
	return plugin.HandshakeConfig{ProtocolVersion: ProtocolVersion, MagicCookieKey: "GCX_PLUGIN", MagicCookieValue: "typed-resources-v1"}
}

// App owns its UI and receives only the typed host capability client.
type App interface {
	Run(ctx context.Context, request *RunRequest, host ResourceHostClient) (*RunResponse, error)
}

func Serve(app App) {
	plugin.Serve(&plugin.ServeConfig{HandshakeConfig: handshake(), Plugins: map[string]plugin.Plugin{"extension": &extensionPlugin{app: app}}, GRPCServer: func(options []grpc.ServerOption) *grpc.Server {
		return grpc.NewServer(append(options, grpc.MaxRecvMsgSize(maxMessageSize))...)
	}, Logger: hclog.NewNullLogger()})
}

type Options struct {
	Binary      string
	Args        []string
	Interactive bool
	Diagnostics io.Writer
}

// Run launches one plugin and serves host callbacks for the duration of its Run RPC.
func Run(ctx context.Context, opts Options, host ResourceHostServer) (*RunResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(opts.Binary) {
		return nil, errors.New("plugin executable must be an absolute path")
	}
	cmd := exec.Command(opts.Binary) //nolint:gosec,noctx // go-plugin owns graceful process cleanup; the caller selects this executable without a shell.
	client := plugin.NewClient(&plugin.ClientConfig{HandshakeConfig: handshake(), Plugins: map[string]plugin.Plugin{"extension": &extensionPlugin{}}, Cmd: cmd, AllowedProtocols: []plugin.Protocol{plugin.ProtocolGRPC}, AutoMTLS: true, GRPCDialOptions: []grpc.DialOption{grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(maxMessageSize))}, StartTimeout: 10 * time.Second, Logger: hclog.NewNullLogger(), Stderr: opts.Diagnostics, SyncStderr: opts.Diagnostics})
	defer client.Kill()
	protocol, err := client.Client()
	if err != nil {
		return nil, fmt.Errorf("start plugin: %w", err)
	}
	raw, err := protocol.Dispense("extension")
	if err != nil {
		return nil, err
	}
	remote, ok := raw.(*pluginRemote)
	if !ok {
		return nil, errors.New("unexpected extension protocol client")
	}
	id := remote.broker.NextId()
	ready := make(chan *grpc.Server, 1)
	go remote.broker.AcceptAndServe(id, func(options []grpc.ServerOption) *grpc.Server {
		server := grpc.NewServer(append(options, grpc.MaxRecvMsgSize(maxMessageSize))...)
		RegisterResourceHostServer(server, host)
		ready <- server
		return server
	})
	startup, cancelStartup := context.WithTimeout(ctx, 10*time.Second)
	defer cancelStartup()
	var server *grpc.Server
	select {
	case server = <-ready:
	case <-startup.Done():
		return nil, fmt.Errorf("start host callbacks: %w", startup.Err())
	}
	defer server.Stop()
	result, err := remote.client.Run(ctx, &RunRequest{HostBrokerId: id, Args: opts.Args, Interactive: opts.Interactive})
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return result, err
}

type extensionPlugin struct {
	plugin.NetRPCUnsupportedPlugin

	app App
}

func (p *extensionPlugin) GRPCServer(broker *plugin.GRPCBroker, server *grpc.Server) error {
	RegisterExtensionServer(server, &extensionServer{app: p.app, broker: broker})
	return nil
}
func (p *extensionPlugin) GRPCClient(_ context.Context, broker *plugin.GRPCBroker, conn *grpc.ClientConn) (any, error) {
	return &pluginRemote{broker: broker, client: NewExtensionClient(conn)}, nil
}

type pluginRemote struct {
	broker *plugin.GRPCBroker
	client ExtensionClient
}
type extensionServer struct {
	UnimplementedExtensionServer

	app    App
	broker *plugin.GRPCBroker
}

func (s *extensionServer) Run(ctx context.Context, req *RunRequest) (*RunResponse, error) {
	conn, err := s.broker.DialWithOptions(req.GetHostBrokerId(), grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(maxMessageSize)))
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	return s.app.Run(ctx, req, NewResourceHostClient(conn))
}
