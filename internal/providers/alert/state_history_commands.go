package alert

import (
	"errors"
	"fmt"
	"strings"
	"time"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/shared"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func stateHistoryCommands(loader GrafanaConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "state-history",
		Short: "Inspect alert state history.",
		Long: `Query the recorded history of alert rule state transitions.

State history is served by Grafana's alerting history backend. The default Loki
backend answers both rule-scoped and global queries; the annotations backend
requires --rule. Records are returned newest-first.

  gcx alert state-history list --rule <uid> --from now-24h
  gcx alert state-history list --label severity=critical --limit 200`,
	}
	cmd.AddCommand(newStateHistoryListCommand(loader))
	return cmd
}

type stateHistoryListOpts struct {
	IO      cmdio.Options
	RuleUID string
	From    string
	To      string
	Limit   int
	Labels  []string
}

func (o *stateHistoryListOpts) setup(flags *pflag.FlagSet) {
	cmdio.RegisterTable(&o.IO, StateHistoryTable())
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.RuleUID, "rule", "", "Filter by rule UID (required by the annotations history backend)")
	flags.StringVar(&o.From, "from", "now-6h", "Start of the time range (RFC3339, Unix seconds, or relative e.g. now-6h)")
	flags.StringVar(&o.To, "to", "now", "End of the time range (RFC3339, Unix seconds, or relative e.g. now)")
	flags.IntVar(&o.Limit, "limit", 100, "Maximum number of records to return (0 for the backend default)")
	flags.StringArrayVar(&o.Labels, "label", nil, "Filter by instance label equality (key=value); repeatable")
}

// resolve validates the options and builds the client query relative to now.
func (o *stateHistoryListOpts) resolve(now time.Time) (StateHistoryOptions, error) {
	if err := o.IO.Validate(); err != nil {
		return StateHistoryOptions{}, err
	}

	from, err := shared.ParseTime(o.From, now)
	if err != nil {
		return StateHistoryOptions{}, fmt.Errorf("invalid --from: %w", err)
	}
	to, err := shared.ParseTime(o.To, now)
	if err != nil {
		return StateHistoryOptions{}, fmt.Errorf("invalid --to: %w", err)
	}
	if !from.IsZero() && !to.IsZero() && to.Before(from) {
		return StateHistoryOptions{}, errors.New("--to must not be before --from")
	}
	if o.Limit < 0 {
		return StateHistoryOptions{}, errors.New("--limit must not be negative")
	}

	labels, err := parseLabelFilters(o.Labels)
	if err != nil {
		return StateHistoryOptions{}, err
	}

	return StateHistoryOptions{
		RuleUID: o.RuleUID,
		From:    from,
		To:      to,
		Limit:   o.Limit,
		Labels:  labels,
	}, nil
}

func newStateHistoryListCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &stateHistoryListOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List recorded alert state transitions.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			query, err := opts.resolve(time.Now())
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			client, err := NewClient(restCfg)
			if err != nil {
				return err
			}

			transitions, err := client.QueryStateHistory(ctx, query)
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), transitions)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// parseLabelFilters turns repeated key=value flags into an equality-filter map.
// The returned map is non-nil (possibly empty) so callers never see nil,nil.
func parseLabelFilters(pairs []string) (map[string]string, error) {
	labels := make(map[string]string, len(pairs))
	for _, p := range pairs {
		k, v, ok := strings.Cut(p, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("invalid --label %q: expected key=value", p)
		}
		labels[k] = v
	}
	return labels, nil
}

func StateHistoryTable() cmdio.Table[StateTransition] {
	return cmdio.Table[StateTransition]{Columns: []cmdio.Column[StateTransition]{
		{Header: "TIME", Content: func(r StateTransition) string { return cmdio.OrDash(formatHistoryTime(r.Time)) }},
		{Header: "RULE_UID", Visible: cmdio.WideOnly, Content: func(r StateTransition) string { return cmdio.OrDash(r.RuleUID) }},
		{Header: "RULE", Content: func(r StateTransition) string { return cmdio.OrDash(r.RuleTitle) }},
		{Header: "PREVIOUS", Content: func(r StateTransition) string { return cmdio.OrDash(r.Previous) }},
		{Header: "CURRENT", Content: func(r StateTransition) string { return cmdio.OrDash(r.Current) }},
		{Header: "ERROR", Visible: cmdio.WideOnly, Content: func(r StateTransition) string { return cmdio.OrDash(r.Error) }},
		{Header: "LABELS", Content: func(r StateTransition) string { return formatLabels(r.Labels) }},
	}}
}

func formatHistoryTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
