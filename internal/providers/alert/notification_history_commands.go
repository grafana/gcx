package alert

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/style"
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
'list' to browse notifications and 'alerts' to see the alerts in a specific one.

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
	if err := validateNotificationStatus(o.Status); err != nil {
		return err
	}
	return validateNotificationOutcome(o.Outcome)
}

func (o *notificationHistoryListOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &NotificationHistoryTableCodec{})
	o.IO.RegisterCustomCodec("wide", &NotificationHistoryTableCodec{Wide: true})
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
	return nil
}

func (o *notificationHistoryAlertsOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &NotificationAlertsTableCodec{})
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
		Use:   "alerts",
		Short: "List the alerts in a single notification.",
		Long: `List the individual alerts that were part of one grouped notification.

The notification's own entry does not carry its alerts, so they are fetched
separately by UUID. The time range must bracket the notification's timestamp;
widen --since (or set --from/--to) if the notification is older.`,
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

// NotificationHistoryTableCodec renders notification history entries as a table.
type NotificationHistoryTableCodec struct {
	Wide bool
}

func (c *NotificationHistoryTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *NotificationHistoryTableCodec) Encode(w io.Writer, v any) error {
	entries, ok := v.([]NotificationEntry)
	if !ok {
		return errors.New("invalid data type for table codec: expected []NotificationEntry")
	}

	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("TIMESTAMP", "RECEIVER", "INTEGRATION", "STATUS", "OUTCOME", "ALERTS", "DURATION", "RULE_UIDS", "GROUP_LABELS", "UUID", "ERROR")
	} else {
		t = style.NewTable("TIMESTAMP", "RECEIVER", "INTEGRATION", "STATUS", "OUTCOME", "ALERTS", "DURATION", "ERROR")
	}

	for _, e := range entries {
		ts := formatTimestamp(e.Timestamp)
		alerts := strconv.FormatInt(e.AlertCount, 10)
		dur := formatDurationNanos(e.Duration)
		errStr := orDash(e.Error)

		if c.Wide {
			ruleUIDs := orDash(strings.Join(e.RuleUIDs, ","))
			t.Row(ts, e.Receiver, e.Integration, e.Status, e.Outcome, alerts, dur, ruleUIDs, formatLabels(e.GroupLabels), orDash(e.UUID), errStr)
			continue
		}

		t.Row(ts, e.Receiver, e.Integration, e.Status, e.Outcome, alerts, dur, errStr)
	}
	return t.Render(w)
}

func (c *NotificationHistoryTableCodec) Decode(r io.Reader, v any) error {
	return errors.New("table format does not support decoding")
}

// NotificationAlertsTableCodec renders the alerts of a single notification.
type NotificationAlertsTableCodec struct{}

func (c *NotificationAlertsTableCodec) Format() format.Format { return "table" }

func (c *NotificationAlertsTableCodec) Encode(w io.Writer, v any) error {
	alerts, ok := v.([]NotificationAlert)
	if !ok {
		return errors.New("invalid data type for table codec: expected []NotificationAlert")
	}

	t := style.NewTable("STATUS", "STARTS_AT", "ENDS_AT", "LABELS")
	for _, a := range alerts {
		t.Row(a.Status, formatTimestamp(a.StartsAt), formatTimestamp(a.EndsAt), formatLabels(a.Labels))
	}
	return t.Render(w)
}

func (c *NotificationAlertsTableCodec) Decode(r io.Reader, v any) error {
	return errors.New("table format does not support decoding")
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
