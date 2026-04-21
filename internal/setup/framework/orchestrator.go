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

// providerParams holds a provider and its collected parameter values.
type providerParams struct {
	provider Setupable
	params   map[string]string
}

// Run is the interactive setup orchestrator.
// It discovers Setupable providers, presents a category multi-select,
// collects parameters per provider, shows a preview, confirms, then calls
// Setup() sequentially for each confirmed provider.
//
// Returns a non-nil error only for unrecoverable failures (e.g., I/O errors).
// Per-provider Setup() failures are recorded in Summary.Failed; Run continues.
// Context cancellation (Ctrl-C) is handled internally and returns (summary, nil).
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

	secretFn := opts.SecretFn
	if secretFn == nil && opts.StdinFile != nil {
		f := opts.StdinFile
		secretFn = func(label string) (string, error) {
			return prompt.Secret(f, errOut, label)
		}
	}

	selectedProviders, selectedIDs, err := selectCategories(ctx, in, out, errOut, providers)
	if err != nil {
		return summary, err
	}
	if selectedProviders == nil {
		return summary, nil
	}

	confirmed, collectionSummary, err := collectAllParams(ctx, in, out, errOut, selectedProviders, selectedIDs, secretFn)
	if err != nil {
		return summary, err
	}
	// Merge partial summary state (Skipped, Cancelled) from collection phase.
	summary.Skipped = append(summary.Skipped, collectionSummary.Skipped...)
	summary.Cancelled = append(summary.Cancelled, collectionSummary.Cancelled...)

	if len(confirmed) == 0 {
		printSummary(errOut, &summary)
		return summary, nil
	}

	ok, err := previewAndConfirm(ctx, in, out, errOut, confirmed)
	if err != nil {
		return summary, err
	}
	if !ok {
		for _, pp := range confirmed {
			summary.Cancelled = append(summary.Cancelled, pp.provider.ProductName())
		}
		printSummary(errOut, &summary)
		return summary, nil
	}

	setupSummary := executeSetups(ctx, errOut, confirmed)
	summary.Completed = append(summary.Completed, setupSummary.Completed...)
	summary.Failed = append(summary.Failed, setupSummary.Failed...)
	summary.NotImplemented = append(summary.NotImplemented, setupSummary.NotImplemented...)
	summary.Cancelled = append(summary.Cancelled, setupSummary.Cancelled...)

	printSummary(errOut, &summary)
	return summary, nil
}

// selectCategories builds the category selection UI, prompts the user, and
// returns the filtered list of providers plus the set of selected category IDs.
// Returns (nil, nil, nil) when there is nothing to do (no categories or nothing selected).
func selectCategories(
	_ context.Context,
	in *bufio.Reader,
	out io.Writer,
	errOut io.Writer,
	providers []Setupable,
) ([]Setupable, map[InfraCategoryID]bool, error) {
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
		return nil, nil, nil
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
		return nil, nil, fmt.Errorf("category selection: %w", err)
	}
	if len(selected) == 0 {
		fmt.Fprintln(errOut, "No categories selected. Nothing to do.")
		return nil, nil, nil
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

	return selectedProviders, selectedIDs, nil
}

// collectAllParams collects parameters for each provider, handling status checks,
// validation retries, and context cancellation. Returns confirmed provider+param
// pairs and a partial Summary capturing Skipped and Cancelled providers.
func collectAllParams(
	ctx context.Context,
	in *bufio.Reader,
	out io.Writer,
	errOut io.Writer,
	providers []Setupable,
	selectedIDs map[InfraCategoryID]bool,
	secretFn func(string) (string, error),
) ([]providerParams, Summary, error) {
	var summary Summary
	confirmed := make([]providerParams, 0, len(providers))

	for _, p := range providers {
		if ctx.Err() != nil {
			summary.Cancelled = append(summary.Cancelled, p.ProductName())
			continue
		}

		status, err := p.Status(ctx)
		if err != nil {
			fmt.Fprintf(errOut, "warning: could not determine status for %s: %v\n", p.ProductName(), err)
			summary.Skipped = append(summary.Skipped, p.ProductName())
			continue
		}
		if status != nil && (status.State == StateConfigured || status.State == StateActive) {
			fmt.Fprintf(errOut, "%s: already configured, skipping\n", p.ProductName())
			summary.Skipped = append(summary.Skipped, p.ProductName())
			continue
		}

		var collectedParams map[string]string
		for ctx.Err() == nil {
			params, err := collectProviderParams(ctx, p, selectedIDs, in, out, errOut, collectedParams, secretFn)
			if err != nil {
				if ctx.Err() != nil {
					break
				}
				return confirmed, summary, fmt.Errorf("collecting params for %s: %w", p.ProductName(), err)
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

	return confirmed, summary, nil
}

// previewAndConfirm displays the setup preview block and asks the user to confirm.
// Returns (false, nil) when the user declines.
func previewAndConfirm(
	_ context.Context,
	in *bufio.Reader,
	out io.Writer,
	_ io.Writer,
	confirmed []providerParams,
) (bool, error) {
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
		return false, fmt.Errorf("confirmation prompt: %w", err)
	}
	return ok, nil
}

// executeSetups calls Setup() sequentially for each confirmed provider and
// returns a Summary capturing the outcomes.
func executeSetups(ctx context.Context, errOut io.Writer, confirmed []providerParams) Summary {
	var summary Summary
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
	return summary
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
			if !param.Secret {
				if v, ok := prev[param.Name]; ok && v != "" {
					def = v
				}
			}

			if param.Secret && param.Kind != ParamKindText && param.Kind != "" {
				return nil, fmt.Errorf("param %q has Secret=true but Kind=%q: Secret is only valid for ParamKindText", param.Name, param.Kind)
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
			default: // ParamKindText
				if param.Secret {
					if secretFn == nil {
						return nil, fmt.Errorf("cannot collect secret param %q: no StdinFile provided", param.Name)
					}
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
