package irm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers/irm/oncalltypes"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ---------------------------------------------------------------------------
// alert-groups command: list, get, actions, list-alerts
// ---------------------------------------------------------------------------

type alertGroupListOpts struct {
	listOpts

	MaxAge string
}

func (o *alertGroupListOpts) setup(flags *pflag.FlagSet) {
	o.listOpts.setup(flags, "alert-groups")
	flags.StringVar(&o.MaxAge, "max-age", "", "Exclude groups older than this duration (e.g. 1h, 24h, 7d)")
}

func newAlertGroupsCommand(loader OnCallConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "alert-groups",
		Short:   "Manage alert groups.",
		Aliases: []string{"alert-group", "ag"},
	}

	cmd.AddCommand(
		newAlertGroupListCommand(loader),
		newAlertGroupListAlertsCommand(loader),
		newGetSubcommand(loader, "Get an alert group by ID.",
			func(ctx context.Context, c OnCallAPI, name string) (*AlertGroup, error) {
				return c.GetAlertGroup(ctx, name)
			}),
		newAlertGroupActionCommand(loader, "acknowledge", "Acknowledge an alert group.", func(c OnCallAPI, cmd *cobra.Command, id string) error {
			return c.AcknowledgeAlertGroup(cmd.Context(), id)
		}),
		newAlertGroupActionCommand(loader, "resolve", "Resolve an alert group.", func(c OnCallAPI, cmd *cobra.Command, id string) error {
			return c.ResolveAlertGroup(cmd.Context(), id)
		}),
		newAlertGroupActionCommand(loader, "unacknowledge", "Unacknowledge an alert group.", func(c OnCallAPI, cmd *cobra.Command, id string) error {
			return c.UnacknowledgeAlertGroup(cmd.Context(), id)
		}),
		newAlertGroupActionCommand(loader, "unresolve", "Unresolve an alert group.", func(c OnCallAPI, cmd *cobra.Command, id string) error {
			return c.UnresolveAlertGroup(cmd.Context(), id)
		}),
		newAlertGroupSilenceCommand(loader),
		newAlertGroupActionCommand(loader, "unsilence", "Unsilence an alert group.", func(c OnCallAPI, cmd *cobra.Command, id string) error {
			return c.UnsilenceAlertGroup(cmd.Context(), id)
		}),
		newAlertGroupActionCommand(loader, "delete", "Delete an alert group.", func(c OnCallAPI, cmd *cobra.Command, id string) error {
			return c.DeleteAlertGroup(cmd.Context(), id)
		}),
	)

	return cmd
}

func newAlertGroupListCommand(loader OnCallConfigLoader) *cobra.Command {
	opts := &alertGroupListOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List alert groups.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			client, namespace, err := loader.LoadOnCallClient(cmd.Context())
			if err != nil {
				return err
			}

			var listOpts []oncalltypes.ListOption
			if opts.MaxAge != "" {
				dur, err := parseDuration(opts.MaxAge)
				if err != nil {
					return fmt.Errorf("invalid --max-age value %q: %w", opts.MaxAge, err)
				}
				cutoff := time.Now().UTC().Add(-dur)
				listOpts = append(listOpts, oncalltypes.WithStartedAfter(cutoff))
			}

			items, err := client.ListAlertGroups(cmd.Context(), listOpts...)
			if err != nil {
				return err
			}

			objs, err := itemsToUnstructured(items, "AlertGroup", "pk", namespace)
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), objs)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func parseDuration(s string) (time.Duration, error) {
	if len(s) > 1 && s[len(s)-1] == 'd' {
		var days int
		if _, err := fmt.Sscanf(s, "%dd", &days); err == nil {
			return time.Duration(days) * 24 * time.Hour, nil
		}
	}
	return time.ParseDuration(s)
}

func newAlertGroupListAlertsCommand(loader OnCallConfigLoader) *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list-alerts <alert-group-id>",
		Short: "List individual alerts for an alert group.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			client, namespace, err := loader.LoadOnCallClient(cmd.Context())
			if err != nil {
				return err
			}

			items, err := client.ListAlerts(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			objs, err := itemsToUnstructured(items, "Alert", "id", namespace)
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), objs)
		},
	}
	opts.setup(cmd.Flags(), "alerts")
	return cmd
}

type alertGroupActionOpts struct {
	IO cmdio.Options
}

func (o *alertGroupActionOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
}

func newAlertGroupActionCommand(loader OnCallConfigLoader, name, short string, actionFn func(OnCallAPI, *cobra.Command, string) error) *cobra.Command {
	opts := &alertGroupActionOpts{}
	cmd := &cobra.Command{
		Use:   name + " <id>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			client, _, err := loader.LoadOnCallClient(cmd.Context())
			if err != nil {
				return err
			}

			id := args[0]
			if err := actionFn(client, cmd, id); err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "Alert group %q %s successfully", id, name)
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func newAlertGroupSilenceCommand(loader OnCallConfigLoader) *cobra.Command {
	opts := &alertGroupActionOpts{}
	var duration int
	cmd := &cobra.Command{
		Use:   "silence <id>",
		Short: "Silence an alert group for a specified duration.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			client, _, err := loader.LoadOnCallClient(cmd.Context())
			if err != nil {
				return err
			}

			id := args[0]
			if err := client.SilenceAlertGroup(cmd.Context(), id, duration); err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "Alert group %q silenced for %d seconds", id, duration)
			return nil
		},
	}
	opts.setup(cmd.Flags())
	cmd.Flags().IntVar(&duration, "duration", 3600, "Duration to silence in seconds")
	return cmd
}

// ---------------------------------------------------------------------------
// final-shifts command (mounted under schedules)
// Uses the internal API filter_events endpoint instead of the public final_shifts endpoint.
// ---------------------------------------------------------------------------

type finalShiftsOpts struct {
	IO    cmdio.Options
	Start string
	End   string
}

func (o *finalShiftsOpts) setup(flags *pflag.FlagSet) {
	today := time.Now().Format("2006-01-02")
	endDate := time.Now().AddDate(0, 0, 7).Format("2006-01-02")
	o.Start = today
	o.End = endDate

	o.IO.RegisterCustomCodec("table", &finalShiftTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Start, "start", o.Start, "Start date (YYYY-MM-DD)")
	flags.StringVar(&o.End, "end", o.End, "End date (YYYY-MM-DD)")
}

func newScheduleFinalShiftsCommand(loader OnCallConfigLoader) *cobra.Command {
	opts := &finalShiftsOpts{}
	cmd := &cobra.Command{
		Use:   "final-shifts <schedule-id>",
		Short: "List final shifts for a schedule.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			client, _, err := loader.LoadOnCallClient(cmd.Context())
			if err != nil {
				return err
			}

			startDate, err := time.Parse("2006-01-02", opts.Start)
			if err != nil {
				return fmt.Errorf("invalid --start date %q: expected YYYY-MM-DD", opts.Start)
			}
			endDate, err := time.Parse("2006-01-02", opts.End)
			if err != nil {
				return fmt.Errorf("invalid --end date %q: expected YYYY-MM-DD", opts.End)
			}
			days := int(endDate.Sub(startDate).Hours()/24) + 1
			if days < 1 {
				return errors.New("--end must be after --start")
			}

			tz := time.Now().Location().String()
			result, err := client.ListFilterEvents(cmd.Context(), args[0], tz, opts.Start, days)
			if err != nil {
				return err
			}

			var shifts []FlatShift
			for _, event := range result.Events {
				if event.IsGap {
					continue
				}
				for _, user := range event.Users {
					shifts = append(shifts, FlatShift{
						UserPK:       user.PK,
						UserEmail:    user.Email,
						UserUsername: user.DisplayName,
						ShiftStart:   event.Start,
						ShiftEnd:     event.End,
					})
				}
			}

			return opts.IO.Encode(cmd.OutOrStdout(), shifts)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// users command: list, get, current
// ---------------------------------------------------------------------------

type usersCurrentOpts struct {
	IO cmdio.Options
}

func (o *usersCurrentOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
}

func newUsersCommand(loader OnCallConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "users",
		Short:   "Manage OnCall users.",
		Aliases: []string{"user"},
	}

	cmd.AddCommand(
		newListSubcommand(loader, "users", "User", "List OnCall users.", "pk",
			func(ctx context.Context, c OnCallAPI) ([]User, error) { return c.ListUsers(ctx) },
			func(ctx context.Context, c OnCallAPI, name string) (*User, error) { return c.GetUser(ctx, name) }),
		newGetSubcommand(loader, "Get a user by ID.",
			func(ctx context.Context, c OnCallAPI, name string) (*User, error) { return c.GetUser(ctx, name) }),
		newUsersCurrentCommand(loader),
	)

	return cmd
}

func newUsersCurrentCommand(loader OnCallConfigLoader) *cobra.Command {
	opts := &usersCurrentOpts{}
	cmd := &cobra.Command{
		Use:   "current",
		Short: "Get the current user.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			client, _, err := loader.LoadOnCallClient(cmd.Context())
			if err != nil {
				return err
			}

			user, err := client.GetCurrentUser(cmd.Context())
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), user)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// escalate command (uses internal API direct_paging endpoint)
// ---------------------------------------------------------------------------

type escalateOpts struct {
	IO        cmdio.Options
	Title     string
	Message   string
	Team      string
	UserIDs   []string
	Important bool
}

func (o *escalateOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Title, "title", "", "Title of the escalation (required)")
	flags.StringVar(&o.Message, "message", "", "Message for the escalation")
	flags.StringVar(&o.Team, "team", "", "Team ID")
	flags.StringSliceVar(&o.UserIDs, "user-ids", nil, "User IDs (comma-separated)")
	flags.BoolVar(&o.Important, "important", false, "Mark as important")
}

func (o *escalateOpts) Validate() error {
	if o.Title == "" {
		return errors.New("--title is required")
	}
	return nil
}

func newEscalateCommand(loader OnCallConfigLoader) *cobra.Command {
	opts := &escalateOpts{}
	cmd := &cobra.Command{
		Use:   "escalate",
		Short: "Create a direct escalation.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			if err := opts.Validate(); err != nil {
				return err
			}

			client, _, err := loader.LoadOnCallClient(cmd.Context())
			if err != nil {
				return err
			}

			// Build internal API input with per-user importance.
			var users []UserReference
			for _, uid := range opts.UserIDs {
				users = append(users, UserReference{
					ID:        uid,
					Important: opts.Important,
				})
			}

			input := DirectPagingInput{
				Title:                   opts.Title,
				Message:                 opts.Message,
				Team:                    opts.Team,
				Users:                   users,
				ImportantTeamEscalation: opts.Important,
			}

			result, err := client.CreateDirectPaging(cmd.Context(), input)
			if err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "Direct escalation created with alert group ID: %s", result.AlertGroupID)
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// itemsToUnstructured + FinalShift table codec
// ---------------------------------------------------------------------------

func itemsToUnstructured[T any](items []T, kind, idField, namespace string) ([]unstructured.Unstructured, error) {
	objs := make([]unstructured.Unstructured, 0, len(items))
	for _, item := range items {
		data, err := json.Marshal(item)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal %s: %w", kind, err)
		}

		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("failed to unmarshal %s: %w", kind, err)
		}

		id := ""
		if v, ok := m[idField]; ok {
			id = fmt.Sprint(v)
		}
		delete(m, idField)

		obj := unstructured.Unstructured{Object: map[string]any{
			"apiVersion": APIVersion,
			"kind":       kind,
			"metadata": map[string]any{
				"name":      id,
				"namespace": namespace,
			},
			"spec": m,
		}}
		objs = append(objs, obj)
	}
	return objs, nil
}

type finalShiftTableCodec struct{ noDecodeCodec }

func (c *finalShiftTableCodec) Format() format.Format { return "table" }

func (c *finalShiftTableCodec) Encode(w io.Writer, v any) error {
	items, ok := v.([]FlatShift)
	if !ok {
		return errors.New("invalid data type for table codec: expected []FlatShift")
	}

	t := style.NewTable("USER_PK", "EMAIL", "USERNAME", "START", "END")
	for _, item := range items {
		start := item.ShiftStart
		if len(start) > 16 {
			start = start[:16]
		}
		end := item.ShiftEnd
		if len(end) > 16 {
			end = end[:16]
		}
		t.Row(item.UserPK, item.UserEmail, item.UserUsername, start, end)
	}
	return t.Render(w)
}
