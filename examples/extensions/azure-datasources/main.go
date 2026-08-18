// Command gcx-ext-azure-datasources is a gcx extension that provisions Grafana
// datasources for Azure Monitor and Azure Data Explorer.
//
// It creates a dedicated Azure app registration per datasource, binds it a
// read-only role at subscription scope, and hands the resulting credential to
// Grafana through gcx. It never sees a Grafana credential itself: every Grafana
// call is made by invoking the gcx binary named in GCX_EXT_GCX_BIN.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"
)

// Exit codes follow gcx's own taxonomy so a caller reading them does not have
// to special-case extensions.
const (
	exitOK        = 0
	exitError     = 1
	exitUsage     = 2
	exitPartial   = 4
	exitCancelled = 5
)

const version = "0.1.0"

type options struct {
	subscription string
	types        []string
	dryRun       bool
	output       string
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	os.Exit(run(ctx, os.Args[1:]))
}

func run(ctx context.Context, argv []string) int {
	if len(argv) == 0 {
		usage(os.Stderr)
		return exitUsage
	}

	verb := argv[0]
	switch verb {
	case "-h", "--help", "help":
		usage(os.Stdout)
		return exitOK
	case "--version", "version":
		fmt.Println(version)
		return exitOK
	case "provision", "cleanup":
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", verb)
		usage(os.Stderr)
		return exitUsage
	}

	o := &options{}
	fs := flag.NewFlagSet("gcx ext "+extensionName()+" "+verb, flag.ContinueOnError)
	fs.StringVar(&o.subscription, "subscription", "", "Azure subscription id or name (default: the active `az` subscription)")
	types := fs.String("types", "", "Restrict to datasource kinds: azure-monitor, adx")
	fs.BoolVar(&o.dryRun, "dry-run", false, "Report what would change without creating or deleting anything")
	fs.StringVar(&o.output, "output", defaultOutput(), "Output format: text or json")
	if err := fs.Parse(argv[1:]); err != nil {
		return exitUsage
	}
	if *types != "" {
		o.types = strings.Split(*types, ",")
	}

	// Progress narration goes to stderr for humans and is suppressed when the
	// caller wants a machine-readable result, matching what gcx does.
	var progress io.Writer
	if o.output == "text" {
		progress = os.Stderr
	}

	g, err := newGCXClient()
	if err != nil {
		return fail(err, o.output)
	}

	var result *runResult
	switch verb {
	case "provision":
		result, err = provision(ctx, o, g, azCLI{}, progress)
	case "cleanup":
		result, err = cleanup(ctx, o, g, azCLI{}, progress)
	}
	if err != nil {
		if ctx.Err() != nil {
			return exitCancelled
		}
		return fail(err, o.output)
	}

	if err := render(os.Stdout, result, o.output); err != nil {
		return fail(err, o.output)
	}
	if partial(result) {
		return exitPartial
	}
	return exitOK
}

func partial(r *runResult) bool {
	for _, d := range r.Datasources {
		if d.Status == "failed" {
			return true
		}
	}
	for _, a := range r.Removed {
		if a.Error != "" {
			return true
		}
	}
	return false
}

func render(w io.Writer, r *runResult, output string) error {
	if output == "json" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(r)
	}

	if r.DryRun {
		fmt.Fprintln(w, "Dry run - nothing was created or deleted.")
	}
	for _, d := range r.Datasources {
		fmt.Fprintf(w, "%-10s %s (%s)\n", d.Status, d.Name, d.Type)
		if d.ClientID != "" {
			fmt.Fprintf(w, "           clientId %s, roles %s at %s\n", d.ClientID, strings.Join(d.Roles, ", "), d.Scope)
		}
		if d.Health != "" {
			fmt.Fprintf(w, "           health %s\n", d.Health)
		}
		if d.Hint != "" {
			fmt.Fprintf(w, "           hint: %s\n", d.Hint)
		}
		if d.Error != "" {
			fmt.Fprintf(w, "           error: %s\n", d.Error)
		}
	}
	for _, a := range r.Removed {
		verb := "removed"
		switch {
		case a.Error != "":
			verb = "failed"
		case a.Planned:
			verb = "would remove"
		}
		fmt.Fprintf(w, "%-14s %s %s\n", verb, a.Kind, a.Name)
		if a.Error != "" {
			fmt.Fprintf(w, "               error: %s\n", a.Error)
		}
	}
	if len(r.Datasources) == 0 && len(r.Removed) == 0 {
		fmt.Fprintln(w, "Nothing to do.")
	}
	return nil
}

// fail reports an error in whichever shape the caller asked for.
func fail(err error, output string) int {
	if output == "json" {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
			"error": map[string]any{"summary": err.Error(), "exitCode": exitError},
		})
		return exitError
	}
	fmt.Fprintln(os.Stderr, "Error: "+err.Error())
	return exitError
}

// defaultOutput mirrors the parent gcx invocation: when gcx is in agent mode
// its extensions should be machine-readable too.
func defaultOutput() string {
	if os.Getenv("GCX_EXT_AGENT_MODE") == "true" {
		return "json"
	}
	return "text"
}

func usage(w io.Writer) {
	name := "gcx ext " + extensionName()
	fmt.Fprintf(w, `Provision Grafana datasources for Azure Monitor and Azure Data Explorer.

Usage:
  %[1]s provision [flags]
  %[1]s cleanup [flags]

Commands:
  provision   Create an app registration per datasource and register it in Grafana
  cleanup     Delete the datasources and app registrations this extension created

Flags:
  --subscription <id|name>   Azure subscription (default: the active az subscription)
  --types <list>             Restrict to azure-monitor, adx
  --dry-run                  Report what would change without changing anything
  --output <text|json>       Output format (default: json under gcx --agent)

Requires the Azure CLI (az), logged in with rights to create app registrations
and assign roles. Grafana access is inherited from gcx: run this through
'gcx ext %[2]s', and use gcx's own --context to pick the stack.
`, name, extensionName())
}

func progressf(w io.Writer, format string, args ...any) {
	if w == nil {
		return
	}
	fmt.Fprintf(w, format+"\n", args...)
}

// sleepCtx waits for d, reporting false if the context was cancelled first.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
