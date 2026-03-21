package oncall

import (
	"errors"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	cmdio "github.com/grafana/grafanactl/cmd/grafanactl/io"
	"github.com/grafana/grafanactl/internal/format"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// ---------------------------------------------------------------------------
// alert-groups command
// ---------------------------------------------------------------------------

type alertGroupActionOpts struct {
	IO cmdio.Options
}

func (o *alertGroupActionOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
}

func newAlertGroupsCommand(loader OnCallConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "alert-groups",
		Short:   "Manage alert groups.",
		Aliases: []string{"ag"},
	}

	cmd.AddCommand(
		newAlertGroupActionCommand(loader, "acknowledge", "Acknowledge an alert group.", func(c *Client, cmd *cobra.Command, id string) error {
			return c.AcknowledgeAlertGroup(cmd.Context(), id)
		}),
		newAlertGroupActionCommand(loader, "resolve", "Resolve an alert group.", func(c *Client, cmd *cobra.Command, id string) error {
			return c.ResolveAlertGroup(cmd.Context(), id)
		}),
		newAlertGroupActionCommand(loader, "unacknowledge", "Unacknowledge an alert group.", func(c *Client, cmd *cobra.Command, id string) error {
			return c.UnacknowledgeAlertGroup(cmd.Context(), id)
		}),
		newAlertGroupActionCommand(loader, "unresolve", "Unresolve an alert group.", func(c *Client, cmd *cobra.Command, id string) error {
			return c.UnresolveAlertGroup(cmd.Context(), id)
		}),
		newAlertGroupSilenceCommand(loader),
		newAlertGroupUnsilenceCommand(loader),
		newAlertGroupDeleteCommand(loader),
	)

	return cmd
}

func newAlertGroupActionCommand(loader OnCallConfigLoader, name, short string, actionFn func(*Client, *cobra.Command, string) error) *cobra.Command {
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

func newAlertGroupUnsilenceCommand(loader OnCallConfigLoader) *cobra.Command {
	opts := &alertGroupActionOpts{}
	cmd := &cobra.Command{
		Use:   "unsilence <id>",
		Short: "Unsilence an alert group.",
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
			if err := client.UnsilenceAlertGroup(cmd.Context(), id); err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "Alert group %q unsilenced", id)
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func newAlertGroupDeleteCommand(loader OnCallConfigLoader) *cobra.Command {
	opts := &alertGroupActionOpts{}
	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete an alert group.",
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
			if err := client.DeleteAlertGroup(cmd.Context(), id); err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "Alert group %q deleted", id)
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// final-shifts command
// ---------------------------------------------------------------------------

type finalShiftsOpts struct {
	IO    cmdio.Options
	Start string
	End   string
}

func (o *finalShiftsOpts) setup(flags *pflag.FlagSet) {
	// Set defaults: today and today+7d
	today := time.Now().Format("2006-01-02")
	tomorrow := time.Now().AddDate(0, 0, 7).Format("2006-01-02")
	o.Start = today
	o.End = tomorrow

	o.IO.RegisterCustomCodec("table", &FinalShiftTableCodec{})
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

			scheduleID := args[0]
			items, err := client.ListFinalShifts(cmd.Context(), scheduleID, opts.Start, opts.End)
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), items)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// users command
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
		Use:   "users",
		Short: "Manage OnCall users.",
	}

	cmd.AddCommand(
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
// escalate command
// ---------------------------------------------------------------------------

type escalateOpts struct {
	IO        cmdio.Options
	Title     string
	Message   string
	TeamID    string
	UserIDs   []string
	Important bool
}

func (o *escalateOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Title, "title", "", "Title of the escalation (required)")
	flags.StringVar(&o.Message, "message", "", "Message for the escalation")
	flags.StringVar(&o.TeamID, "team-id", "", "Team ID")
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

			input := DirectEscalationInput{
				Title:     opts.Title,
				Message:   opts.Message,
				TeamID:    opts.TeamID,
				UserIDs:   opts.UserIDs,
				Important: opts.Important,
			}

			result, err := client.CreateDirectEscalation(cmd.Context(), input)
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
// FinalShiftTableCodec
// ---------------------------------------------------------------------------

// FinalShiftTableCodec renders final shifts as a table.
type FinalShiftTableCodec struct{}

func (c *FinalShiftTableCodec) Format() format.Format { return "table" }

func (c *FinalShiftTableCodec) Encode(w io.Writer, v any) error {
	items, ok := v.([]FinalShift)
	if !ok {
		return errors.New("invalid data type for table codec: expected []FinalShift")
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "USER_PK\tEMAIL\tUSERNAME\tSTART\tEND")

	for _, item := range items {
		start := item.ShiftStart
		if len(start) > 16 {
			start = start[:16]
		}
		end := item.ShiftEnd
		if len(end) > 16 {
			end = end[:16]
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", item.UserPK, item.UserEmail, item.UserUsername, start, end)
	}

	return tw.Flush()
}

func (c *FinalShiftTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}
