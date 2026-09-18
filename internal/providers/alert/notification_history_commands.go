package alert

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// notificationHistoryCommands returns the notification-history command group.
func notificationHistoryCommands(loader GrafanaConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "notification-history",
		Aliases: []string{"nh"},
		Short:   "Inspect alert notification delivery history.",
		Long: `Inspect the history of alert notifications delivered by Grafana Alerting.

These commands are read-only. Each entry is a grouped notification that Grafana
attempted to send to a contact point, recorded by the alerting historian. Use
'list' to browse notifications and 'list-alerts' to see the alerts in a specific one.

Notification history must be enabled on the stack (the
[unified_alerting.notification_history] config with Loki, plus the
kubernetesAlertingHistorian feature).`,
	}
	cmd.AddCommand(
		newNotificationHistoryListCommand(loader),
		newNotificationHistoryAlertsCommand(loader),
	)
	return cmd
}

// ---------------------------------------------------------------------------
// list
// ---------------------------------------------------------------------------

type notificationHistoryListOpts struct {
	IO       cmdio.Options
	From     string
	To       string
	Since    time.Duration
	Limit    int64
	Receiver string
	Status   string
	Outcome  string
	RuleUID  string
}

func (o *notificationHistoryListOpts) Validate() error {
	if err := o.IO.Validate(); err != nil {
		return err
	}
	if o.Limit < 0 {
		return errors.New("--limit must be non-negative")
	}
	if err := validateNotificationStatus(o.Status); err != nil {
		return err
	}
	return validateNotificationOutcome(o.Outcome)
}

func (o *notificationHistoryListOpts) setup(flags *pflag.FlagSet) {
	cmdio.RegisterTable(&o.IO, NotificationHistoryTable())
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.From, "from", "", "Start of time range (RFC3339). Overrides --since.")
	flags.StringVar(&o.To, "to", "", "End of time range (RFC3339, default now).")
	flags.DurationVar(&o.Since, "since", time.Hour, "Look back this far from now when --from is not set.")
	flags.Int64Var(&o.Limit, "limit", 100, "Maximum number of notifications to return.")
	flags.StringVar(&o.Receiver, "receiver", "", "Filter by contact point (receiver) name.")
	flags.StringVar(&o.Status, "status", "", "Filter by notification status (firing, resolved).")
	flags.StringVar(&o.Outcome, "outcome", "", "Filter by delivery outcome (success, error).")
	flags.StringVar(&o.RuleUID, "rule-uid", "", "Filter by alert rule UID.")
}

func newNotificationHistoryListCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &notificationHistoryListOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List notification delivery history.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			from, to, err := resolveNotificationTimeRange(opts.From, opts.To, opts.Since)
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

			entries, err := client.QueryNotifications(ctx, NotificationQueryRequest{
				From:     from,
				To:       to,
				Limit:    opts.Limit,
				Receiver: opts.Receiver,
				Status:   opts.Status,
				Outcome:  opts.Outcome,
				RuleUID:  opts.RuleUID,
			})
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), entries)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// alerts
// ---------------------------------------------------------------------------

type notificationHistoryAlertsOpts struct {
	IO    cmdio.Options
	UUID  string
	From  string
	To    string
	Since time.Duration
	Limit int64
}

func (o *notificationHistoryAlertsOpts) Validate() error {
	if err := o.IO.Validate(); err != nil {
		return err
	}
	if o.UUID == "" {
		return errors.New("--uuid is required")
	}
	if o.Limit < 0 {
		return errors.New("--limit must be non-negative")
	}
	return nil
}

func (o *notificationHistoryAlertsOpts) setup(flags *pflag.FlagSet) {
	cmdio.RegisterTable(&o.IO, NotificationAlertsTable())
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.UUID, "uuid", "", "UUID of the notification (from 'notification-history list').")
	flags.StringVar(&o.From, "from", "", "Start of time range (RFC3339). Overrides --since.")
	flags.StringVar(&o.To, "to", "", "End of time range (RFC3339, default now).")
	flags.DurationVar(&o.Since, "since", time.Hour, "Look back this far from now when --from is not set.")
	flags.Int64Var(&o.Limit, "limit", 100, "Maximum number of alerts to return.")
}

func newNotificationHistoryAlertsCommand(loader GrafanaConfigLoader) *cobra.Command {
	opts := &notificationHistoryAlertsOpts{}
	cmd := &cobra.Command{
		Use:   "list-alerts",
		Short: "List the alerts in a single notification.",
		Long: `List the individual alerts that were part of one grouped notification.

The notification's own entry does not carry its alerts, so they are fetched
separately by UUID. The time range must bracket the notification's timestamp;
widen --since (or set --from/--to) if the notification is older.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			from, to, err := resolveNotificationTimeRange(opts.From, opts.To, opts.Since)
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

			alerts, err := client.QueryAlerts(ctx, NotificationAlertsRequest{
				UUID:  opts.UUID,
				From:  from,
				To:    to,
				Limit: opts.Limit,
			})
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), alerts)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// codecs
// ---------------------------------------------------------------------------

func NotificationHistoryTable() cmdio.Table[NotificationEntry] {
	return cmdio.Table[NotificationEntry]{Columns: []cmdio.Column[NotificationEntry]{
		{Header: "TIMESTAMP", Content: func(r NotificationEntry) string { return formatTimestamp(r.Timestamp) }},
		{Header: "RECEIVER", Content: func(r NotificationEntry) string { return r.Receiver }},
		{Header: "INTEGRATION", Content: func(r NotificationEntry) string { return r.Integration }},
		{Header: "STATUS", Content: func(r NotificationEntry) string { return r.Status }},
		{Header: "OUTCOME", Content: func(r NotificationEntry) string { return r.Outcome }},
		{Header: "ALERTS", Content: func(r NotificationEntry) string { return strconv.FormatInt(r.AlertCount, 10) }},
		{Header: "DURATION", Content: func(r NotificationEntry) string { return formatDurationNanos(r.Duration) }},
		{Header: "RULE_UIDS", Visible: cmdio.WideOnly, Content: func(r NotificationEntry) string { return cmdio.OrDash(strings.Join(r.RuleUIDs, ",")) }},
		{Header: "GROUP_LABELS", Visible: cmdio.WideOnly, Content: func(r NotificationEntry) string { return formatLabels(r.GroupLabels) }},
		{Header: "UUID", Visible: cmdio.WideOnly, Content: func(r NotificationEntry) string { return cmdio.OrDash(r.UUID) }},
		{Header: "ERROR", Content: func(r NotificationEntry) string { return cmdio.OrDash(r.Error) }},
	}}
}

func NotificationAlertsTable() cmdio.Table[NotificationAlert] {
	return cmdio.Table[NotificationAlert]{Columns: []cmdio.Column[NotificationAlert]{
		{Header: "STATUS", Content: func(r NotificationAlert) string { return r.Status }},
		{Header: "STARTS_AT", Content: func(r NotificationAlert) string { return formatTimestamp(r.StartsAt) }},
		{Header: "ENDS_AT", Content: func(r NotificationAlert) string { return formatTimestamp(r.EndsAt) }},
		{Header: "LABELS", Content: func(r NotificationAlert) string { return formatLabels(r.Labels) }},
	}}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func validateNotificationStatus(status string) error {
	switch status {
	case "", NotificationStatusFiring, NotificationStatusResolved:
		return nil
	default:
		return fmt.Errorf("invalid status %q: must be one of firing, resolved", status)
	}
}

func validateNotificationOutcome(outcome string) error {
	switch outcome {
	case "", NotificationOutcomeSuccess, NotificationOutcomeError:
		return nil
	default:
		return fmt.Errorf("invalid outcome %q: must be one of success, error", outcome)
	}
}

// resolveNotificationTimeRange resolves the effective [from, to] window. When
// --from is empty it defaults to to.Add(-since); when --to is empty it defaults
// to now.
func resolveNotificationTimeRange(from, to string, since time.Duration) (time.Time, time.Time, error) {
	toTime := time.Now().UTC()
	if to != "" {
		parsed, err := time.Parse(time.RFC3339, to)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid --to %q: must be RFC3339: %w", to, err)
		}
		toTime = parsed
	}

	var fromTime time.Time
	if from != "" {
		parsed, err := time.Parse(time.RFC3339, from)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid --from %q: must be RFC3339: %w", from, err)
		}
		fromTime = parsed
	} else {
		fromTime = toTime.Add(-since)
	}

	if !fromTime.Before(toTime) {
		return time.Time{}, time.Time{}, fmt.Errorf("time range is empty: from (%s) must be before to (%s)",
			fromTime.Format(time.RFC3339), toTime.Format(time.RFC3339))
	}
	return fromTime, toTime, nil
}

// formatTimestamp renders a timestamp as UTC RFC3339, or "-" when zero.
func formatTimestamp(t time.Time) string {
	if t.IsZero() || t.Year() <= 1 {
		return "-"
	}
	return t.UTC().Format(time.RFC3339)
}

// formatDurationNanos renders a nanosecond duration rounded to milliseconds.
func formatDurationNanos(ns int64) string {
	if ns == 0 {
		return "-"
	}
	return time.Duration(ns).Round(time.Millisecond).String()
}
