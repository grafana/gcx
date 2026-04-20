package framework

import (
	"bufio"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"slices"
	"strings"

	"github.com/grafana/gcx/internal/setup/framework/prompt"
	"github.com/grafana/gcx/internal/terminal"
)

// Summary is the structured result returned by Run.
type Summary struct {
	Completed      []string // providers whose Setup() succeeded
	Failed         []string // providers whose Setup() returned a non-stub error
	NotImplemented []string // providers whose Setup() returned ErrSetupNotSupported
	Skipped        []string // providers skipped because already configured/active
	Cancelled      []string // providers not reached due to Ctrl-C or user refusal
}

// Options configures a Run invocation.
type Options struct {
	In        io.Reader
	StdinFile *os.File    // needed for secret prompts; may be nil (disables Secret kind)
	Out       io.Writer   // informational output
	Err       io.Writer   // error/warning output
	Providers []Setupable // if nil, DiscoverSetupable() is used
	// IsInteractive overrides the default TTY check (terminal.StdinIsTerminal).
	// Useful for tests that inject a bytes.Buffer as stdin.
	IsInteractive func() bool
	// SecretFn overrides the default prompt.Secret call.
	// Signature: func(label string) (string, error).
	// When nil and StdinFile is set, prompt.Secret is used.
	SecretFn func(label string) (string, error)
}

// Run is the interactive setup orchestrator.
// It discovers Setupable providers, presents a category multi-select,
// collects parameters per provider, shows a preview, confirms, then calls
// Setup() sequentially for each confirmed provider.
//
// Returns a non-nil error only for unrecoverable failures (e.g., I/O errors).
// Per-provider Setup() failures are recorded in Summary.Failed; Run continues.
// Context cancellation (Ctrl-C) is handled internally and returns (summary, nil).
//
//nolint:gocyclo,maintidx
func Run(ctx context.Context, opts Options) (Summary, error) {
	var summary Summary

	isInteractive := terminal.StdinIsTerminal
	if opts.IsInteractive != nil {
		isInteractive = opts.IsInteractive
	}
	if !isInteractive() {
		return summary, errors.New("gcx setup run requires an interactive terminal (stdin is not a TTY)")
	}

	// Wrap ctx with signal handling so Ctrl-C cancels the run cleanly.
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()

	out := opts.Out
	if out == nil {
		out = os.Stdout
	}
	errOut := opts.Err
	if errOut == nil {
		errOut = os.Stderr
	}

	// Use a single bufio.Reader for all interactive prompt input.
	var in *bufio.Reader
	if opts.In != nil {
		if br, ok := opts.In.(*bufio.Reader); ok {
			in = br
		} else {
			in = bufio.NewReader(opts.In)
		}
	} else {
		in = bufio.NewReader(os.Stdin)
	}

	providers := opts.Providers
	if providers == nil {
		providers = DiscoverSetupable()
	}

	// Sort providers alphabetically so iteration order is deterministic.
	slices.SortFunc(providers, func(a, b Setupable) int {
		return cmp.Compare(a.ProductName(), b.ProductName())
	})

	// Build the category map (first-label-wins for duplicate IDs).
	type catEntry struct {
		id    InfraCategoryID
		label string
	}
	seen := make(map[InfraCategoryID]bool)
	var catOrder []catEntry
	for _, p := range providers {
		for _, c := range p.InfraCategories() {
			if !seen[c.ID] {
				seen[c.ID] = true
				catOrder = append(catOrder, catEntry{id: c.ID, label: c.Label})
			}
		}
	}

	if len(catOrder) == 0 {
		fmt.Fprintln(errOut, "No interactive setup flows available.")
		return summary, nil
	}

	// Build label list and reverse-map for user selection.
	catLabels := make([]string, len(catOrder))
	labelToID := make(map[string]InfraCategoryID, len(catOrder))
	for i, c := range catOrder {
		catLabels[i] = c.label
		labelToID[c.label] = c.id
	}

	selected, err := prompt.MultiChoice(in, out, "Select infrastructure categories to set up:", catLabels, catLabels)
	if err != nil {
		return summary, fmt.Errorf("category selection: %w", err)
	}
	if len(selected) == 0 {
		fmt.Fprintln(errOut, "No categories selected. Nothing to do.")
		return summary, nil
	}

	selectedIDs := make(map[InfraCategoryID]bool, len(selected))
	for _, label := range selected {
		selectedIDs[labelToID[label]] = true
	}

	// Filter providers to those with at least one selected category.
	var selectedProviders []Setupable
	for _, p := range providers {
		for _, c := range p.InfraCategories() {
			if selectedIDs[c.ID] {
				selectedProviders = append(selectedProviders, p)
				break
			}
		}
	}

	secretFn := opts.SecretFn
	if secretFn == nil && opts.StdinFile != nil {
		f := opts.StdinFile
		secretFn = func(label string) (string, error) {
			return prompt.Secret(f, errOut, label)
		}
	}

	// Per-provider parameter collection.
	type providerParams struct {
		provider Setupable
		params   map[string]string
	}
	confirmed := make([]providerParams, 0, len(selectedProviders))

	for _, p := range selectedProviders {
		if ctx.Err() != nil {
			summary.Cancelled = append(summary.Cancelled, p.ProductName())
			continue
		}

		status, err := p.Status(ctx)
		if err == nil && status != nil {
			if status.State == StateConfigured || status.State == StateActive {
				fmt.Fprintf(errOut, "%s: already configured, skipping\n", p.ProductName())
				summary.Skipped = append(summary.Skipped, p.ProductName())
				continue
			}
		}

		var collectedParams map[string]string
		for ctx.Err() == nil {
			params, err := collectProviderParams(ctx, p, selectedIDs, in, out, errOut, collectedParams, secretFn)
			if err != nil {
				if ctx.Err() != nil {
					break
				}
				return summary, fmt.Errorf("collecting params for %s: %w", p.ProductName(), err)
			}
			collectedParams = params

			if validateErr := p.ValidateSetup(ctx, params); validateErr != nil {
				fmt.Fprintf(errOut, "Validation error for %s: %v\n", p.ProductName(), validateErr)
				continue
			}
			break
		}

		if ctx.Err() != nil {
			summary.Cancelled = append(summary.Cancelled, p.ProductName())
			continue
		}

		confirmed = append(confirmed, providerParams{provider: p, params: collectedParams})
	}

	if len(confirmed) == 0 {
		printSummary(errOut, &summary)
		return summary, nil
	}

	// Preview block.
	fmt.Fprintln(out, "\n=== Setup Preview ===")
	for _, pp := range confirmed {
		fmt.Fprintf(out, "\n%s:\n", pp.provider.ProductName())
		if len(pp.params) == 0 {
			fmt.Fprintln(out, "  (no parameters)")
		} else {
			for _, cat := range pp.provider.InfraCategories() {
				for _, param := range cat.Params {
					val := pp.params[param.Name]
					if param.Secret {
						val = "***"
					}
					fmt.Fprintf(out, "  %s: %s\n", param.Name, val)
				}
			}
		}
	}
	fmt.Fprintln(out, "")

	ok, err := prompt.Bool(in, out, "Continue?", true)
	if err != nil {
		return summary, fmt.Errorf("confirmation prompt: %w", err)
	}
	if !ok {
		for _, pp := range confirmed {
			summary.Cancelled = append(summary.Cancelled, pp.provider.ProductName())
		}
		printSummary(errOut, &summary)
		return summary, nil
	}

	// Sequential Setup invocations.
	for _, pp := range confirmed {
		if ctx.Err() != nil {
			summary.Cancelled = append(summary.Cancelled, pp.provider.ProductName())
			continue
		}
		fmt.Fprintf(errOut, "Setting up %s...\n", pp.provider.ProductName())
		if err := pp.provider.Setup(ctx, pp.params); err != nil {
			if errors.Is(err, ErrSetupNotSupported) {
				fmt.Fprintf(errOut, "%s: setup not yet implemented\n", pp.provider.ProductName())
				summary.NotImplemented = append(summary.NotImplemented, pp.provider.ProductName())
			} else {
				fmt.Fprintf(errOut, "%s: setup failed: %v\n", pp.provider.ProductName(), err)
				summary.Failed = append(summary.Failed, pp.provider.ProductName())
			}
			continue
		}
		summary.Completed = append(summary.Completed, pp.provider.ProductName())
	}

	printSummary(errOut, &summary)
	return summary, nil
}

// collectProviderParams collects all parameters for a single provider from
// the user. prev contains previously-collected values used as defaults on retry.
func collectProviderParams(
	ctx context.Context,
	p Setupable,
	selectedIDs map[InfraCategoryID]bool,
	in *bufio.Reader,
	out io.Writer,
	errOut io.Writer,
	prev map[string]string,
	secretFn func(string) (string, error),
) (map[string]string, error) {
	params := make(map[string]string)

	for _, cat := range p.InfraCategories() {
		if !selectedIDs[cat.ID] {
			continue
		}
		for _, param := range cat.Params {
			if ctx.Err() != nil {
				return params, ctx.Err()
			}

			def := param.Default
			if v, ok := prev[param.Name]; ok && v != "" {
				def = v
			}

			var val string
			var err error

			switch param.Kind {
			case ParamKindBool:
				defBool := def == "true"
				b, bErr := prompt.Bool(in, out, param.Prompt, defBool)
				if bErr != nil {
					return nil, bErr
				}
				if b {
					val = "true"
				} else {
					val = "false"
				}
			case ParamKindChoice:
				choices := param.Choices
				if len(choices) == 0 {
					resolved, rErr := p.ResolveChoices(ctx, param.Name)
					if rErr != nil {
						fmt.Fprintf(errOut, "warning: could not resolve choices for %s: %v\n", param.Name, rErr)
					} else {
						choices = resolved
					}
				}
				val, err = prompt.Choice(in, out, param.Prompt, choices, def)
				if err != nil {
					return nil, err
				}
			case ParamKindMultiChoice:
				choices := param.Choices
				if len(choices) == 0 {
					resolved, rErr := p.ResolveChoices(ctx, param.Name)
					if rErr != nil {
						fmt.Fprintf(errOut, "warning: could not resolve choices for %s: %v\n", param.Name, rErr)
					} else {
						choices = resolved
					}
				}
				var defs []string
				if def != "" {
					defs = []string{def}
				}
				vals, mErr := prompt.MultiChoice(in, out, param.Prompt, choices, defs)
				if mErr != nil {
					return nil, mErr
				}
				val = strings.Join(vals, ",")
			default: // ParamKindText and Secret
				if param.Secret && secretFn != nil {
					val, err = secretFn(param.Prompt)
					if err != nil {
						return nil, err
					}
				} else {
					val, err = prompt.Text(in, out, param.Prompt, def, param.Required)
					if err != nil {
						return nil, err
					}
				}
			}

			params[param.Name] = val
		}
	}
	return params, nil
}

func printSummary(w io.Writer, s *Summary) {
	if len(s.Completed) > 0 {
		fmt.Fprintf(w, "\nCompleted: %v\n", s.Completed)
	}
	if len(s.Failed) > 0 {
		fmt.Fprintf(w, "Failed: %v\n", s.Failed)
	}
	if len(s.NotImplemented) > 0 {
		fmt.Fprintf(w, "Not implemented: %v\n", s.NotImplemented)
	}
	if len(s.Skipped) > 0 {
		fmt.Fprintf(w, "Skipped: %v\n", s.Skipped)
	}
	if len(s.Cancelled) > 0 {
		fmt.Fprintf(w, "Cancelled: %v\n", s.Cancelled)
	}
}
