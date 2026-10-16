package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"runtime"

	tea "charm.land/bubbletea/v2"
	"github.com/grafana/gcx-ext-resource-explorer/browse"
	sdk "github.com/grafana/gcx/experimental/extensionsdk"
	"golang.org/x/term"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--fixture" {
		os.Exit(fixture(os.Args[2:], os.Stdout, os.Stderr))
	}
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("resource-explorer", flag.ContinueOnError)
	demo := fs.Bool("demo", false, "Use offline resources through a fixture subprocess")
	noTUI := fs.Bool("no-tui", false, "Print types, schemas and the first type's objects as JSON")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: gcx ext resource-explorer [--demo] [--no-tui] [RESOURCE_SELECTOR]...\nRead-only resource/schema explorer. Enter: objects; /: filter; tab: preview; o: open; q: quit.")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	commands, err := sdk.FromEnvironment()
	if *demo {
		self, selfErr := os.Executable()
		err = selfErr
		if err == nil {
			commands, err = sdk.New(sdk.Options{Binary: self, Prefix: []string{"--fixture"}, Context: "demo"})
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	src := &cliSource{commands: commands, selectors: fs.Args()}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if *noTUI {
		if err := report(ctx, src, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	if os.Getenv("GCX_AGENT_MODE") == "true" || !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		fmt.Fprintln(os.Stderr, "resource-explorer needs an interactive terminal; use --no-tui")
		return 2
	}
	opener := func(url string) error { return openURL(ctx, url) }
	if _, err := tea.NewProgram(browse.New(ctx, src, opener), tea.WithContext(ctx)).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
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
		objects, listErr := src.List(ctx, types[0])
		if listErr != nil {
			var partial *browse.PartialError
			if !errors.As(listErr, &partial) {
				return listErr
			}
			result["warning"] = listErr.Error()
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
