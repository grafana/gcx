package azure

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/grafana/gcx/internal/gcxerrors"
	azonboard "github.com/grafana/gcx/internal/onboard/azure"
	cmdio "github.com/grafana/gcx/internal/output"
)

// resolveSubscriptions returns the subscriptions to operate on.
func resolveSubscriptions(ctx context.Context, cli *azonboard.CLI, opts *azureOpts, interactive bool) ([]azonboard.Account, error) {
	all, err := cli.ListSubscriptions(ctx)
	if err != nil || len(all) == 0 {
		// Fall back to the current account.
		cur, cerr := cli.CurrentAccount(ctx)
		if cerr != nil {
			if err != nil {
				return nil, err
			}
			return nil, cerr
		}
		all = []azonboard.Account{cur}
	}

	if len(opts.Subscription) > 0 {
		want := map[string]bool{}
		for _, s := range opts.Subscription {
			want[strings.ToLower(s)] = true
		}
		var filtered []azonboard.Account
		for _, a := range all {
			if want[strings.ToLower(a.SubID)] {
				filtered = append(filtered, a)
			}
		}
		if len(filtered) == 0 {
			return nil, gcxerrors.DetailedError{
				Summary:     "none of the requested subscriptions were found",
				Suggestions: []string{"List subscriptions with: az account list -o table"},
			}
		}
		return filtered, nil
	}

	if !interactive || len(all) == 1 {
		return all, nil
	}

	// Interactive multi-select, default all selected.
	var opted []string
	options := make([]huh.Option[string], 0, len(all))
	for _, a := range all {
		label := fmt.Sprintf("%s (%s)", a.Name, a.SubID)
		options = append(options, huh.NewOption(label, a.SubID).Selected(true))
		opted = append(opted, a.SubID)
	}
	sel := opted
	form := huh.NewForm(huh.NewGroup(
		huh.NewMultiSelect[string]().Title("Subscriptions to onboard").Options(options...).Value(&sel),
	))
	if err := form.Run(); err != nil {
		return nil, err
	}
	chosen := map[string]bool{}
	for _, id := range sel {
		chosen[id] = true
	}
	var out []azonboard.Account
	for _, a := range all {
		if chosen[a.SubID] {
			out = append(out, a)
		}
	}
	return out, nil
}

// resolveSelections filters suggestions by --types, lets the user choose
// (interactive; default all selectable), and resolves the role set per
// selection. Disabled suggestions (e.g. stopped ADX clusters) are shown in the
// interactive picker but cannot be selected, and are skipped with a warning in
// non-interactive mode.
func resolveSelections(suggestions []azonboard.Suggestion, opts *azureOpts, interactive bool, errOut io.Writer) ([]azonboard.Selection, error) {
	suggestions = filterByType(suggestions, opts.Types)
	if len(suggestions) == 0 {
		return nil, nil
	}

	var chosen []azonboard.Suggestion
	if interactive {
		picked, err := pickSuggestions(suggestions)
		if err != nil {
			return nil, err
		}
		chosen = picked
	} else {
		for _, s := range suggestions {
			if s.Disabled {
				cmdio.EmitWarn(errOut, fmt.Sprintf("skipping %s: %s", s.Label, s.DisabledReason))
				continue
			}
			chosen = append(chosen, s)
		}
	}

	selections := make([]azonboard.Selection, 0, len(chosen))
	for _, s := range chosen {
		if s.Disabled {
			continue // defensive: never provision a disabled suggestion
		}
		roles, err := resolveRoles(s, opts, interactive)
		if err != nil {
			return nil, err
		}
		selections = append(selections, azonboard.Selection{Suggestion: s, Roles: roles})
	}
	return selections, nil
}

func resolveRoles(s azonboard.Suggestion, opts *azureOpts, interactive bool) ([]string, error) {
	if opts.Role != "" {
		return splitRoles(opts.Role), nil
	}
	options := s.Spec.RoleOptions()
	if len(options) == 0 {
		return nil, nil
	}
	if interactive && len(options) > 1 {
		labels := make([]huh.Option[int], 0, len(options))
		for i, ro := range options {
			labels = append(labels, huh.NewOption(ro.Label, i))
		}
		idx := 0
		form := huh.NewForm(huh.NewGroup(
			huh.NewSelect[int]().Title("Permissions for " + s.Label).Options(labels...).Value(&idx),
		))
		if err := form.Run(); err != nil {
			return nil, err
		}
		return options[idx].Roles, nil
	}
	return options[0].Roles, nil
}

func filterByType(suggestions []azonboard.Suggestion, types []string) []azonboard.Suggestion {
	if len(types) == 0 {
		return suggestions
	}
	want := map[string]bool{}
	for _, o := range types {
		want[strings.ToLower(strings.TrimSpace(o))] = true
	}
	var out []azonboard.Suggestion
	for _, s := range suggestions {
		if want[s.Spec.Token()] {
			out = append(out, s)
		}
	}
	return out
}

func splitRoles(s string) []string {
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// normalizeAccount fills in fields missing from `az account list` (notably
// environmentName) using the originally-active account as a fallback.
func normalizeAccount(a, fallback azonboard.Account) azonboard.Account {
	if a.CloudName == "" {
		a.CloudName = fallback.CloudName
	}
	if a.CloudName == "" {
		a.CloudName = "AzureCloud"
	}
	if a.TenantID == "" {
		a.TenantID = fallback.TenantID
	}
	return a
}
