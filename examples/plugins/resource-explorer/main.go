package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os/exec"
	"runtime"

	tea "charm.land/bubbletea/v2"
	"github.com/grafana/gcx-plugin-resource-explorer/browse"
	sdk "github.com/grafana/gcx/experimental/pluginsdk"
)

func main() { sdk.Serve(explorer{}) }

type explorer struct{}

func (explorer) Run(ctx context.Context, request *sdk.RunRequest, host sdk.ResourceHostClient) (*sdk.RunResponse, error) {
	var diagnostics bytes.Buffer
	flags := flag.NewFlagSet("resource-explorer", flag.ContinueOnError)
	flags.SetOutput(&diagnostics)
	noTUI := flags.Bool("no-tui", false, "Print types, schemas and the first type's objects as JSON")
	flags.Usage = func() {
		fmt.Fprintln(&diagnostics, "Usage: gcx plugins run [--demo] <executable> -- [--no-tui] [RESOURCE_SELECTOR]...\nEnter: objects; /: filter; tab: preview; o: open; q: quit.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(request.GetArgs()); err != nil {
		code := int32(2)
		if errors.Is(err, flag.ErrHelp) {
			code = 0
		}
		return &sdk.RunResponse{ExitCode: code, Stderr: diagnostics.String()}, nil
	}
	src := &rpcSource{host: host, selectors: flags.Args()}
	if *noTUI {
		var output bytes.Buffer
		if err := report(ctx, src, &output); err != nil {
			return &sdk.RunResponse{ExitCode: 1, Stderr: err.Error()}, nil
		}
		return &sdk.RunResponse{Stdout: output.Bytes()}, nil
	}
	if !request.GetInteractive() {
		return &sdk.RunResponse{ExitCode: 2, Stderr: "resource-explorer needs an interactive terminal; use --no-tui"}, nil
	}
	terminal, err := openTerminal()
	if err != nil {
		return nil, err
	}
	defer terminal.Close()
	opener := func(url string) error { return openURL(ctx, url) }
	_, err = tea.NewProgram(browse.New(ctx, src, opener), tea.WithContext(ctx), tea.WithInput(terminal), tea.WithOutput(terminal)).Run()
	if err != nil {
		return &sdk.RunResponse{ExitCode: 1, Stderr: err.Error()}, nil
	}
	return &sdk.RunResponse{}, nil
}
func report(ctx context.Context, src browse.Source, w io.Writer) error {
	types, err := src.Types(ctx)
	if err != nil {
		return err
	}
	schemas, schemaErr := src.Schemas(ctx, types)
	result := map[string]any{"types": types, "schemas": schemas}
	if schemaErr != nil {
		result["schemaWarning"] = schemaErr.Error()
	}
	if len(types) > 0 {
		objects, err := src.List(ctx, types[0])
		if err != nil {
			var partial *browse.PartialError
			if !errors.As(err, &partial) {
				return err
			}
			result["warning"] = err.Error()
		}
		result["objects"] = objects
	}
	return json.NewEncoder(w).Encode(result)
}
func openURL(ctx context.Context, url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "open", url)
	case "windows":
		cmd = exec.CommandContext(ctx, "rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.CommandContext(ctx, "xdg-open", url)
	}
	return cmd.Run()
}
