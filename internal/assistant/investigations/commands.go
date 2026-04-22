package investigations

import (
	"errors"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	"github.com/grafana/gcx/internal/deeplink"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/limit"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func newClient(cmd *cobra.Command, loader *providers.ConfigLoader) (*Client, error) {
	cfg, err := loader.LoadGrafanaConfig(cmd.Context())
	if err != nil {
		return nil, err
	}
	base, err := assistanthttp.NewClient(cfg)
	if err != nil {
		return nil, err
	}
	return NewClient(base), nil
}

// Commands returns the investigations command group.
func Commands(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "investigations",
		Short: "Manage Grafana Assistant investigations.",
	}

	cmd.AddCommand(
		newListCommand(loader),
		newGetCommand(loader),
		newCreateCommand(loader),
		newCancelCommand(loader),
		newTodosCommand(loader),
		newTimelineCommand(loader),
		newReportCommand(loader),
		newDocumentCommand(loader),
		newApprovalsCommand(loader),
	)
	return cmd
}

// --- list ---

type listOpts struct {
	IO     cmdio.Options
	State  string
	Offset int
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &ListTableCodec{})
	o.IO.RegisterCustomCodec("wide", &ListTableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.State, "state", "", "Filter by investigation state (e.g. running, completed, cancelled)")
	flags.IntVar(&o.Offset, "offset", 0, "Number of investigations to skip (for pagination)")
}

func newListCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List investigations.",
		Long:  "List investigations with optional state filter.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			resolvedLimit := int(limit.Resolve(cmd.Context(), 50))
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			summaries, err := client.List(cmd.Context(), ListOptions{
				State:  opts.State,
				Limit:  resolvedLimit,
				Offset: opts.Offset,
			})
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), summaries)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- get ---

type getOpts struct {
	IO   cmdio.Options
	Open bool
}

func (o *getOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
	flags.BoolVar(&o.Open, "open", false, "Open the investigation in the default browser")
}

func newGetCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &getOpts{}
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Get investigation detail.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			if opts.Open {
				cfg, err := loader.LoadGrafanaConfig(cmd.Context())
				if err != nil {
					return err
				}
				url := deeplink.Resolve(cfg.GrafanaURL, deeplink.InvestigationGVK(), args[0])
				if url == "" {
					return fmt.Errorf("no deep link URL available for investigation %s", args[0])
				}
				cmdio.Info(cmd.ErrOrStderr(), "Opening %s", url)
				return deeplink.Open(url)
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			inv, err := client.Get(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), inv)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- create ---

type createOpts struct {
	IO          cmdio.Options
	Title       string
	Description string
}

func (o *createOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Title, "title", "", "Investigation title (required)")
	flags.StringVar(&o.Description, "description", "", "Investigation description")
}

func (o *createOpts) Validate() error {
	if o.Title == "" {
		return errors.New("--title is required")
	}
	return nil
}

func newCreateCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &createOpts{}
	cmd := &cobra.Command{
		Use:     "create",
		Short:   "Create a new investigation.",
		Long:    "Create a new investigation with a title and optional description.",
		Example: `  gcx assistant investigations create --title="High CPU usage" --description="Investigating CPU spikes on prod"`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			resp, err := client.Create(cmd.Context(), CreateRequest{
				Title:       opts.Title,
				Description: opts.Description,
			})
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- cancel ---

type cancelOpts struct {
	IO cmdio.Options
}

func (o *cancelOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
}

func newCancelCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &cancelOpts{}
	cmd := &cobra.Command{
		Use:   "cancel <id>",
		Short: "Cancel a running investigation.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			resp, err := client.Cancel(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- todos ---

type todosOpts struct {
	IO cmdio.Options
}

func (o *todosOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &TodosTableCodec{})
	o.IO.RegisterCustomCodec("wide", &TodosTableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newTodosCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &todosOpts{}
	cmd := &cobra.Command{
		Use:   "todos <id>",
		Short: "Show agent tasks for an investigation.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			todos, err := client.Todos(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), todos)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- timeline ---

type timelineOpts struct {
	IO cmdio.Options
}

func (o *timelineOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &TimelineTableCodec{})
	o.IO.RegisterCustomCodec("wide", &TimelineTableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newTimelineCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &timelineOpts{}
	cmd := &cobra.Command{
		Use:   "timeline <id>",
		Short: "Show activity timeline for an investigation.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			agents, err := client.Timeline(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), agents)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- report ---

type reportOpts struct {
	IO cmdio.Options
}

func (o *reportOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
}

func newReportCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &reportOpts{}
	cmd := &cobra.Command{
		Use:   "report <id>",
		Short: "Show condensed report summary for an investigation.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			report, err := client.Report(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), report)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- document ---

type documentOpts struct {
	IO cmdio.Options
}

func (o *documentOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
}

func newDocumentCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &documentOpts{}
	cmd := &cobra.Command{
		Use:   "document <investigation-id> <document-id>",
		Short: "Fetch a specific investigation document.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			doc, err := client.Document(cmd.Context(), args[0], args[1])
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), doc)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- approvals ---

type approvalsOpts struct {
	IO cmdio.Options
}

func (o *approvalsOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &ApprovalsTableCodec{})
	o.IO.RegisterCustomCodec("wide", &ApprovalsTableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newApprovalsCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &approvalsOpts{}
	cmd := &cobra.Command{
		Use:   "approvals <id>",
		Short: "List approval requests for an investigation.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			approvals, err := client.Approvals(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), approvals)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- table codecs ---

// ListTableCodec renders []InvestigationSummary as a table.
type ListTableCodec struct {
	Wide bool
}

func (c *ListTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *ListTableCodec) Encode(w io.Writer, v any) error {
	summaries, ok := v.([]InvestigationSummary)
	if !ok {
		return errors.New("invalid data type for table codec: expected []InvestigationSummary")
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if c.Wide {
		fmt.Fprintln(tw, "ID\tTITLE\tSTATUS\tCREATED BY\tCREATED\tUPDATED")
	} else {
		fmt.Fprintln(tw, "ID\tTITLE\tSTATUS\tUPDATED")
	}

	for _, s := range summaries {
		title := truncate(s.Title, 40)
		updated := assistanthttp.FormatTime(s.UpdatedAt)

		if c.Wide {
			created := assistanthttp.FormatTime(s.CreatedAt)
			createdBy := "-"
			if s.Source != nil && s.Source.UserID != "" {
				createdBy = s.Source.UserID
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
				s.ID, title, s.State, createdBy, created, updated)
		} else {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
				s.ID, title, s.State, updated)
		}
	}
	return tw.Flush()
}

func (c *ListTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// TodosTableCodec renders []Todo as a table.
type TodosTableCodec struct {
	Wide bool
}

func (c *TodosTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *TodosTableCodec) Encode(w io.Writer, v any) error {
	todos, ok := v.([]Todo)
	if !ok {
		return errors.New("invalid data type for table codec: expected []Todo")
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if c.Wide {
		fmt.Fprintln(tw, "ID\tTITLE\tSTATUS\tASSIGNEE")
	} else {
		fmt.Fprintln(tw, "ID\tTITLE\tSTATUS")
	}

	for _, todo := range todos {
		title := truncate(todo.Title, 50)
		if c.Wide {
			assignee := todo.Assignee
			if assignee == "" {
				assignee = "-"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", todo.ID, title, todo.Status, assignee)
		} else {
			fmt.Fprintf(tw, "%s\t%s\t%s\n", todo.ID, title, todo.Status)
		}
	}
	return tw.Flush()
}

func (c *TodosTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// TimelineTableCodec renders []TimelineEntry as a table.
type TimelineTableCodec struct {
	Wide bool
}

func (c *TimelineTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *TimelineTableCodec) Encode(w io.Writer, v any) error {
	agents, ok := v.([]TimelineAgent)
	if !ok {
		return errors.New("invalid data type for table codec: expected []TimelineAgent")
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if c.Wide {
		fmt.Fprintln(tw, "AGENT ID\tNAME\tSTATUS\tMESSAGES\tSTARTED\tLAST ACTIVITY")
	} else {
		fmt.Fprintln(tw, "AGENT ID\tNAME\tSTATUS\tMESSAGES")
	}

	for _, a := range agents {
		name := truncate(a.AgentName, 40)
		if c.Wide {
			started := assistanthttp.FormatMillis(a.StartTime)
			lastAct := assistanthttp.FormatMillis(a.LastActivity)
			fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\t%s\n", a.AgentID, name, a.Status, a.MessageCount, started, lastAct)
		} else {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%d\n", a.AgentID, name, a.Status, a.MessageCount)
		}
	}
	return tw.Flush()
}

func (c *TimelineTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// ApprovalsTableCodec renders []Approval as a table.
type ApprovalsTableCodec struct {
	Wide bool
}

func (c *ApprovalsTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *ApprovalsTableCodec) Encode(w io.Writer, v any) error {
	approvals, ok := v.([]Approval)
	if !ok {
		return errors.New("invalid data type for table codec: expected []Approval")
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tSTATUS\tAPPROVER\tCREATED")

	for _, a := range approvals {
		approver := a.Approver
		if approver == "" {
			approver = "-"
		}
		created := assistanthttp.FormatTime(a.CreatedAt)
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", a.ID, a.Status, approver, created)
	}
	return tw.Flush()
}

func (c *ApprovalsTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

func truncate(s string, maxLen int) string {
	if s == "" {
		return "-"
	}
	r := []rune(s)
	if len(r) > maxLen {
		return string(r[:maxLen-3]) + "..."
	}
	return s
}
