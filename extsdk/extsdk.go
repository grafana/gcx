// Package extsdk is the Go SDK for gcx extensions.
//
// An extension is an executable that calls [Serve] from main. gcx starts it
// with go-plugin when a user runs `gcx ext <name> [args...]` and passes it
// the arguments that followed the name. The extension reads Grafana data by
// running gcx commands through [GCX], so it does not need to handle
// credentials.
package extsdk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/grafana/gcx/extsdk/internal/extensionpb"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
)

// ProtocolVersion is the extension protocol version this SDK speaks. gcx
// refuses to start an extension built for a different version.
const ProtocolVersion = 1

// The handshake values must match those in gcx's internal/extensions package.
const (
	magicCookieKey   = "GCX_EXTENSION"
	magicCookieValue = "gcx-extension"
	pluginName       = "extension"
)

// RunFunc implements an extension. args are the arguments that followed the
// extension name. ctx is cancelled when gcx is interrupted.
//
// Output written to os.Stdout and os.Stderr is forwarded to gcx's own
// streams. A returned error is printed to stderr. A nil error exits 0, an
// [*ExitError] exits with its code and any other error exits 1.
type RunFunc func(ctx context.Context, gcx *GCX, args []string) error

// Serve runs the extension. It must be called from main and does not return
// until gcx stops the extension.
func Serve(run RunFunc) {
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: handshake(),
		Plugins:         map[string]plugin.Plugin{pluginName: &extensionPlugin{run: run}},
		GRPCServer:      plugin.DefaultGRPCServer,
		Logger:          hclog.NewNullLogger(),
	})
}

// Request is one gcx command.
type Request struct {
	// Args are gcx arguments without the binary name, e.g.
	// {"resources", "get", "dashboards"}. They are not interpreted by a shell.
	Args []string
	// Stdin is passed to the command's standard input.
	Stdin []byte
	// Stdout, when set, receives the command's standard output as it is
	// written, and Result.Stdout stays empty. Use it to read a stream, such
	// as server-sent events from `gcx api`, before the command exits.
	Stdout io.Writer
}

// Result is the captured output of a gcx command.
type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// Err returns an [*ExitError] when the command exited non-zero. The message
// comes from gcx's JSON error document when there is one, else from stderr.
func (r *Result) Err() error {
	if r.ExitCode == 0 {
		return nil
	}
	// gcx prefixes its own error messages, and so does Serve. Later lines
	// hold details and suggestions meant for a terminal.
	msg, _, _ := strings.Cut(strings.TrimSpace(string(r.Stderr)), "\n")
	msg = strings.TrimSpace(strings.TrimPrefix(msg, "Error: "))
	var doc struct {
		Error struct {
			Summary string `json:"summary"`
			Details string `json:"details"`
		} `json:"error"`
	}
	if json.Unmarshal(r.Stdout, &doc) == nil && doc.Error.Summary != "" {
		msg = doc.Error.Summary
		if doc.Error.Details != "" {
			msg += ": " + doc.Error.Details
		}
	}
	if msg == "" {
		msg = fmt.Sprintf("gcx exited with status %d", r.ExitCode)
	}
	return &ExitError{Code: r.ExitCode, Message: msg}
}

// ExitError is a non-zero exit status. Return one from a [RunFunc] to exit
// with a specific code from 1 to 255. Any other code exits 1.
type ExitError struct {
	Code    int
	Message string
}

func (e *ExitError) Error() string { return e.Message }

// GCX runs gcx commands on behalf of the extension. Each command runs in a
// new gcx process that uses the context and configuration the extension was
// started with.
type GCX struct {
	host extensionpb.HostClient
}

// Execute runs a gcx command. A command that exits non-zero returns its
// output and exit code with a nil error, because gcx can report a partial
// failure alongside a complete result. The error is non-nil only when the
// command could not be run.
func (g *GCX) Execute(ctx context.Context, req Request) (*Result, error) {
	stream, err := g.host.Execute(ctx, &extensionpb.ExecuteRequest{Args: req.Args, Stdin: req.Stdin})
	if err != nil {
		return nil, rpcError(ctx, err)
	}
	var stdout, stderr bytes.Buffer
	for {
		event, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				err = errors.New("gcx ended the command without an exit code")
			}
			return nil, rpcError(ctx, err)
		}
		if out := event.GetOutput(); out != nil {
			switch {
			case out.GetStream() == extensionpb.OutputChunk_STREAM_STDERR:
				stderr.Write(out.GetData())
			case req.Stdout != nil:
				if _, err := req.Stdout.Write(out.GetData()); err != nil {
					return nil, fmt.Errorf("write gcx output: %w", err)
				}
			default:
				stdout.Write(out.GetData())
			}
			continue
		}
		return &Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), ExitCode: int(event.GetExitCode())}, nil
	}
}

func rpcError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return err
}

// DecodeJSON runs a gcx command with --output json and decodes its stdout
// into out. It fails if the command exits non-zero.
func (g *GCX) DecodeJSON(ctx context.Context, out any, args ...string) error {
	res, err := g.Execute(ctx, Request{Args: append(append([]string(nil), args...), "--output", "json")})
	if err != nil {
		return err
	}
	if err := res.Err(); err != nil {
		return err
	}
	if err := json.Unmarshal(res.Stdout, out); err != nil {
		return fmt.Errorf("decode gcx JSON output: %w", err)
	}
	return nil
}

func handshake() plugin.HandshakeConfig {
	return plugin.HandshakeConfig{
		ProtocolVersion:  ProtocolVersion,
		MagicCookieKey:   magicCookieKey,
		MagicCookieValue: magicCookieValue,
	}
}

type extensionPlugin struct {
	plugin.NetRPCUnsupportedPlugin

	run RunFunc
}

func (p *extensionPlugin) GRPCServer(broker *plugin.GRPCBroker, s *grpc.Server) error {
	extensionpb.RegisterExtensionServer(s, &extensionServer{run: p.run, broker: broker})
	return nil
}

func (p *extensionPlugin) GRPCClient(context.Context, *plugin.GRPCBroker, *grpc.ClientConn) (any, error) {
	return nil, errors.New("extsdk only serves extensions")
}

type extensionServer struct {
	extensionpb.UnimplementedExtensionServer

	run    RunFunc
	broker *plugin.GRPCBroker
}

func (s *extensionServer) Run(ctx context.Context, req *extensionpb.RunRequest) (*extensionpb.RunResponse, error) {
	conn, err := s.broker.Dial(req.GetHostBrokerId())
	if err != nil {
		return nil, fmt.Errorf("connect to gcx: %w", err)
	}
	defer conn.Close()

	host := extensionpb.NewHostClient(conn)
	output, err := host.Output(ctx)
	if err != nil {
		return nil, fmt.Errorf("connect to gcx output: %w", err)
	}
	code, err := captureOutput(output, func() int {
		return exitCode(s.run(ctx, &GCX{host: host}, req.GetArgs()))
	})
	if err != nil {
		return nil, fmt.Errorf("send output to gcx: %w", err)
	}
	return &extensionpb.RunResponse{ExitCode: int32(code)}, nil //nolint:gosec // exit codes fit in int32
}

// captureOutput sends everything written to os.Stdout and os.Stderr during
// fn to gcx, and returns once gcx has received it. go-plugin's own stdio
// forwarding can drop output written just before Run returns.
func captureOutput(stream extensionpb.Host_OutputClient, fn func() int) (int, error) {
	stdout, stderr := os.Stdout, os.Stderr
	defer func() { os.Stdout, os.Stderr = stdout, stderr }()

	var mu sync.Mutex // gRPC streams do not support concurrent sends
	var sendErr error
	var wg sync.WaitGroup
	forward := func(target **os.File, kind extensionpb.OutputChunk_Stream) error {
		r, w, err := os.Pipe()
		if err != nil {
			return err
		}
		*target = w
		wg.Go(func() {
			defer r.Close()
			buf := make([]byte, 32<<10)
			for {
				n, err := r.Read(buf)
				if n > 0 {
					mu.Lock()
					if sendErr == nil {
						sendErr = stream.Send(&extensionpb.OutputChunk{Stream: kind, Data: buf[:n]})
					}
					mu.Unlock()
				}
				if err != nil {
					return
				}
			}
		})
		return nil
	}
	if err := forward(&os.Stdout, extensionpb.OutputChunk_STREAM_STDOUT); err != nil {
		return 0, err
	}
	if err := forward(&os.Stderr, extensionpb.OutputChunk_STREAM_STDERR); err != nil {
		os.Stdout.Close()
		wg.Wait()
		return 0, err
	}

	code := fn()
	os.Stdout.Close()
	os.Stderr.Close()
	wg.Wait()
	if sendErr != nil {
		return code, sendErr
	}
	_, err := stream.CloseAndRecv()
	return code, err
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	code := 1
	var exit *ExitError
	// Codes outside 1-255 would wrap or read as success, so they exit 1.
	if errors.As(err, &exit) && exit.Code >= 1 && exit.Code <= 255 {
		code = exit.Code
	}
	if msg := err.Error(); msg != "" {
		fmt.Fprintln(os.Stderr, "Error:", msg)
	}
	return code
}
