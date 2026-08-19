// Command gcx-ext-profile-explorer is a gcx extension: a terminal flamegraph
// explorer for Pyroscope profiles. It holds no credentials and speaks no
// Pyroscope API — every fetch is a `gcx datasources pyroscope …` call back
// through the gcx binary that dispatched it.
//
// The flamegraph navigation model (absolute-offset levels, zoom as a root
// frame, sub-cell frames skipped during navigation) follows
// https://github.com/simonswine/lptm.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
)

// Exit codes match gcx's taxonomy so a caller does not have to special-case
// this extension.
const (
	exitOK        = 0
	exitError     = 1
	exitUsage     = 2
	exitCancelled = 5
)

type startArgs struct {
	datasource  string
	expr        string
	profileType string
	since       string
	top         int
	noTUI       bool
}

func main() {
	os.Exit(run())
}

func run() int {
	args, err := parseArgs(os.Args[1:])
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		return exitUsage
	}

	client, err := newGCXClient()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return exitUsage
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if args.noTUI || !interactive() {
		if err := report(ctx, client, args); err != nil {
			if ctx.Err() != nil {
				return exitCancelled
			}
			fmt.Fprintln(os.Stderr, "error:", err)
			return exitError
		}
		return exitOK
	}

	p := tea.NewProgram(newModel(ctx, client, args), tea.WithAltScreen(), tea.WithContext(ctx))
	if _, err := p.Run(); err != nil {
		if errors.Is(err, tea.ErrProgramKilled) || ctx.Err() != nil {
			return exitCancelled
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		return exitError
	}
	return exitOK
}

func parseArgs(argv []string) (startArgs, error) {
	var a startArgs
	fs := flag.NewFlagSet(extensionName(), flag.ContinueOnError)
	fs.StringVar(&a.datasource, "datasource", "", "Pyroscope datasource UID (skips the datasource picker)")
	fs.StringVar(&a.datasource, "d", "", "shorthand for --datasource")
	fs.StringVar(&a.expr, "expr", "", "label selector to load on start, e.g. '{service_name=\"frontend\"}'")
	fs.StringVar(&a.profileType, "profile-type", "", "profile type ID to select on start")
	fs.StringVar(&a.since, "since", "1h", "time range to query ("+strings.Join(sinceOptions, ", ")+")")
	fs.IntVar(&a.top, "top", 20, "rows to print when not attached to a terminal")
	fs.BoolVar(&a.noTUI, "no-tui", false, "print the top functions as JSON instead of opening the TUI")
	fs.Usage = func() {
		name := extensionName()
		_, _ = fmt.Fprintf(fs.Output(), "Explore Pyroscope profiles as an interactive flamegraph.\n\n"+
			"Usage:\n  gcx ext %s [flags]\n\nFlags:\n", name)
		fs.PrintDefaults()
		_, _ = fmt.Fprintf(fs.Output(), "\nExamples:\n"+
			"  # Pick a datasource, then a service, interactively\n"+
			"  gcx ext %s\n\n"+
			"  # Jump straight to one service's CPU profile\n"+
			"  gcx ext %s -d grafanacloud-profiles \\\n"+
			"    --expr '{service_name=\"frontend\"}' \\\n"+
			"    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds\n\n"+
			"  # Non-interactive: heaviest functions as JSON\n"+
			"  gcx ext %s -d grafanacloud-profiles --expr '{service_name=\"frontend\"}' --no-tui\n\n"+
			"Press ? inside the TUI for key bindings.\n", name, name, name)
	}
	if err := fs.Parse(argv); err != nil {
		return a, err
	}
	if sinceOptions[sinceIndex(a.since)] != a.since {
		return a, fmt.Errorf("unsupported --since %q: use one of %s", a.since, strings.Join(sinceOptions, ", "))
	}
	return a, nil
}

// interactive reports whether stdout is a terminal and the parent is not an
// agent, which is what decides between the TUI and the JSON report.
func interactive() bool {
	if os.Getenv("GCX_EXT_AGENT_MODE") == "true" {
		return false
	}
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

type reportRow struct {
	Name     string  `json:"name"`
	Self     int64   `json:"self"`
	Total    int64   `json:"total"`
	SelfPct  float64 `json:"selfPercent"`
	TotalPct float64 `json:"totalPercent"`
}

// report is the non-interactive path: resolve what the TUI would have let the
// user pick, then print the heaviest functions as one JSON document.
func report(ctx context.Context, client *gcxClient, args startArgs) error {
	if args.expr == "" {
		return errors.New("--expr is required when not attached to a terminal")
	}

	ds := args.datasource
	if ds == "" {
		found, err := client.profilingDatasources(ctx)
		if err != nil {
			return err
		}
		if len(found) > 1 {
			names := make([]string, 0, len(found))
			for _, d := range found {
				names = append(names, d.UID)
			}
			return fmt.Errorf("this context has %d Pyroscope datasources: pass -d one of %s",
				len(found), strings.Join(names, ", "))
		}
		ds = found[0].UID
	}

	types, err := client.profileTypes(ctx, ds)
	if err != nil {
		return err
	}
	pt := types[defaultTypeIndex(types, args.profileType)]
	if args.profileType != "" && pt.ID != args.profileType {
		return fmt.Errorf("datasource %s has no profile type %q", ds, args.profileType)
	}

	fg, err := client.flamegraph(ctx, query{
		datasource:  ds,
		expr:        args.expr,
		profileType: pt,
		since:       args.since,
		maxNodes:    maxNodes,
	})
	if err != nil {
		return err
	}

	n := &nav{width: 200}
	rows := topFunctions(fg, n, args.top)
	out := struct {
		Context     string      `json:"context,omitempty"`
		Datasource  string      `json:"datasource"`
		ProfileType string      `json:"profileType"`
		Query       string      `json:"query"`
		Since       string      `json:"since"`
		Unit        string      `json:"unit"`
		Total       int64       `json:"total"`
		Top         []reportRow `json:"top"`
	}{
		Context:     client.context,
		Datasource:  ds,
		ProfileType: pt.ID,
		Query:       args.expr,
		Since:       args.since,
		Unit:        fg.unit,
		Total:       fg.total,
		Top:         make([]reportRow, 0, len(rows)),
	}
	for _, r := range rows {
		out.Top = append(out.Top, reportRow{
			Name:     r.Name,
			Self:     r.Self,
			Total:    r.Total,
			SelfPct:  percent(r.Self, fg.total),
			TotalPct: percent(r.Total, fg.total),
		})
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
