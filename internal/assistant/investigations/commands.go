package investigations

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	"github.com/grafana/gcx/internal/deeplink"
	"github.com/grafana/gcx/internal/format"
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

// printV2Hint writes a one-line v2-successor hint to stderr when the connected
// stack is known to support the /api/v2 investigations surface. Reads from
// cache only — no network. The `hint:` prefix and stderr channel match the
// convention in internal/output (Options.Encode): agents read these and adjust
// subsequent invocations, so we emit unconditionally rather than suppressing
// in agent or piped modes.
func printV2Hint(cmd *cobra.Command, loader *providers.ConfigLoader, message string) {
	if !CachedAPIMode(cmd.Context(), loader).SupportsV2() {
		return
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "hint: %s\n", message)
}

// loadClientAndAPIMode returns a client configured for the detected API mode.
// Used by the auto-dispatching commands (list, get, create).
func loadClientAndAPIMode(cmd *cobra.Command, loader *providers.ConfigLoader) (*Client, APIMode, error) {
	cfg, err := loader.LoadGrafanaConfig(cmd.Context())
	if err != nil {
		return nil, "", err
	}
	base, err := assistanthttp.NewClient(cfg)
	if err != nil {
		return nil, "", err
	}
	mode, err := DetectAPIMode(cmd.Context(), loader, base)
	if err != nil {
		return nil, "", err
	}
	return NewClient(base), mode, nil
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
		// v2-only commands. Each probes capability at run time and returns
		// a friendly error when run against a v1-only stack.
		newPauseCommand(loader),
		newResumeCommand(loader),
		newModeCommand(loader),
		newShareCommand(loader),
		newRegenerateReportCommand(loader),
		newChatCommand(loader),
		newNarrativeCommand(loader),
		newToolsCommand(loader),
	)
	return cmd
}

// --- list ---

type listOpts struct {
	IO     cmdio.Options
	State  string
	Limit  int
	Offset int

	// v2-only filters. Setting any of these on a v1 stack is rejected.
	Scope         string
	Team          string
	Q             string
	From          string
	To            string
	Sort          string
	Order         string
	View          string
	Label         string
	IncludeLegacy bool

	// Flags actually present on the command line, used to detect v2-only
	// filters set against a v1 stack so we can reject with a clear error.
	setFlags map[string]bool
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &ListTableCodec{})
	o.IO.RegisterCustomCodec("wide", &ListTableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.State, "state", "", "Filter by investigation state (comma-separated, or \"all\")")
	flags.IntVar(&o.Limit, "limit", 50, "Maximum number of investigations to return")
	flags.IntVar(&o.Offset, "offset", 0, "Number of investigations to skip (for pagination)")
	flags.StringVar(&o.Scope, "scope", "", "Visibility scope: all|mine|teams|system (v2 only)")
	flags.StringVar(&o.Team, "team", "", "Filter to a specific team (v2 only)")
	flags.StringVar(&o.Q, "q", "", "Search text across title, description, chat name (v2 only)")
	flags.StringVar(&o.From, "from", "", "Lower bound on creation time, RFC3339 (v2 only)")
	flags.StringVar(&o.To, "to", "", "Upper bound on creation time, RFC3339 (v2 only)")
	flags.StringVar(&o.Sort, "sort", "", "Sort field: createdAt|updatedAt|title|state (v2 only)")
	flags.StringVar(&o.Order, "order", "", "Sort order: asc|desc (v2 only)")
	flags.StringVar(&o.View, "view", "", "Result detail level: full|lite (v2 only)")
	flags.StringVar(&o.Label, "label", "", "Filter by label, key:value format (v2 only)")
	flags.BoolVar(&o.IncludeLegacy, "include-legacy", true, "Include legacy (pre-v2) investigations (v2 only)")
}

// captureSetFlags records which flags the user actually set, so v2-only flags
// surfaced on a v1 stack can be rejected without flagging defaults.
func (o *listOpts) captureSetFlags(flags *pflag.FlagSet) {
	o.setFlags = map[string]bool{}
	flags.Visit(func(f *pflag.Flag) { o.setFlags[f.Name] = true })
}

// validateForV1 errors out when the user set a v2-only filter on a v1 stack.
func (o *listOpts) validateForV1() error {
	v2Only := []string{"scope", "team", "q", "from", "to", "sort", "order", "view", "label"}
	for _, name := range v2Only {
		if o.setFlags[name] {
			return fmt.Errorf("--%s is only supported on stacks with the v2 investigations API enabled", name)
		}
	}
	return nil
}

// validateForV2 errors out when v2 flag values don't match the allowed enum.
func (o *listOpts) validateForV2() error {
	validScopes := []string{"all", "mine", "teams", "system"}
	validViews := []string{"full", "lite"}
	validOrders := []string{"asc", "desc"}
	validSorts := []string{"createdAt", "updatedAt", "title", "state"}
	if o.Scope != "" && !slices.Contains(validScopes, o.Scope) {
		return fmt.Errorf("invalid --scope %q: must be one of %s", o.Scope, strings.Join(validScopes, ", "))
	}
	if o.View != "" && !slices.Contains(validViews, o.View) {
		return fmt.Errorf("invalid --view %q: must be one of %s", o.View, strings.Join(validViews, ", "))
	}
	if o.Order != "" && !slices.Contains(validOrders, o.Order) {
		return fmt.Errorf("invalid --order %q: must be one of %s", o.Order, strings.Join(validOrders, ", "))
	}
	if o.Sort != "" && !slices.Contains(validSorts, o.Sort) {
		return fmt.Errorf("invalid --sort %q: must be one of %s", o.Sort, strings.Join(validSorts, ", "))
	}
	return nil
}

func newListCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List investigations.",
		Long:  "List investigations. Auto-detects whether the stack supports the v2 investigations API and uses the richer endpoint when available.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			opts.captureSetFlags(cmd.Flags())
			client, mode, err := loadClientAndAPIMode(cmd, loader)
			if err != nil {
				return err
			}
			if !mode.SupportsV2() {
				if err := opts.validateForV1(); err != nil {
					return err
				}
				summaries, err := client.List(cmd.Context(), ListOptions{
					State:  opts.State,
					Limit:  opts.Limit,
					Offset: opts.Offset,
				})
				if err != nil {
					return err
				}
				return opts.IO.Encode(cmd.OutOrStdout(), summaries)
			}
			if err := opts.validateForV2(); err != nil {
				return err
			}
			summaries, err := client.ListLodestone(cmd.Context(), ListLodestoneOptions{
				State:         opts.State,
				Q:             opts.Q,
				Scope:         opts.Scope,
				TeamName:      opts.Team,
				From:          opts.From,
				To:            opts.To,
				Sort:          opts.Sort,
				Order:         opts.Order,
				View:          opts.View,
				Label:         opts.Label,
				Limit:         opts.Limit,
				Offset:        opts.Offset,
				IncludeLegacy: opts.IncludeLegacy,
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
		Long:  "Get investigation detail. On v2-enabled stacks, returns the full session state when the ID is a v2 investigation, and falls back to legacy detail otherwise.",
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
			client, mode, err := loadClientAndAPIMode(cmd, loader)
			if err != nil {
				return err
			}
			if mode.SupportsV2() {
				resp, status, err := client.ResolveByID(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				if status == http.StatusOK {
					state, err := client.GetState(cmd.Context(), resp.InvestigationID)
					if err != nil {
						return err
					}
					return opts.IO.Encode(cmd.OutOrStdout(), state)
				}
				// 404 — not a v2 investigation; fall through to legacy detail.
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
	Instruction string
	Description string
	Teams       []string
	ProfileID   string

	setFlags map[string]bool
}

func (o *createOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Title, "title", "", "Investigation title")
	flags.StringVar(&o.Instruction, "instruction", "", "Investigation instruction (required on v2-enabled stacks)")
	flags.StringVar(&o.Description, "description", "", "Investigation description (legacy alias of --instruction)")
	flags.StringSliceVar(&o.Teams, "team", nil, "Team name to scope the investigation to (repeatable, v2 only)")
	flags.StringVar(&o.ProfileID, "profile-id", "", "Runner profile ID (v2 only)")
}

func (o *createOpts) captureSetFlags(flags *pflag.FlagSet) {
	o.setFlags = map[string]bool{}
	flags.Visit(func(f *pflag.Flag) { o.setFlags[f.Name] = true })
}

// Validate runs checks shared by both v1 and v2 paths. Call before the
// capability-conditional validateForV1 / validateForV2 helpers.
func (o *createOpts) Validate() error {
	if o.setFlags["instruction"] && o.setFlags["description"] {
		return errors.New("--instruction and --description are mutually exclusive")
	}
	return nil
}

func (o *createOpts) validateForV1() error {
	for _, name := range []string{"team", "profile-id"} {
		if o.setFlags[name] {
			return fmt.Errorf("--%s is only supported on stacks with the v2 investigations API enabled", name)
		}
	}
	if o.Title == "" {
		return errors.New("--title is required")
	}
	return nil
}

func (o *createOpts) validateForV2() error {
	if o.Instruction == "" && o.Description == "" {
		return errors.New("--instruction is required")
	}
	return nil
}

func newCreateCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &createOpts{}
	cmd := &cobra.Command{
		Use:     "create",
		Short:   "Create a new investigation.",
		Long:    "Create a new investigation. On v2-enabled stacks, uses the v2 API with --instruction; falls back to legacy create otherwise.",
		Example: `  gcx assistant investigations create --instruction="Debug API latency spike" --team=sre`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			opts.captureSetFlags(cmd.Flags())
			if err := opts.Validate(); err != nil {
				return err
			}
			client, mode, err := loadClientAndAPIMode(cmd, loader)
			if err != nil {
				return err
			}
			if !mode.SupportsV2() {
				if err := opts.validateForV1(); err != nil {
					return err
				}
				description := opts.Description
				if description == "" {
					description = opts.Instruction
				}
				resp, err := client.Create(cmd.Context(), CreateRequest{
					Title:       opts.Title,
					Description: description,
				})
				if err != nil {
					return err
				}
				return opts.IO.Encode(cmd.OutOrStdout(), resp)
			}
			if err := opts.validateForV2(); err != nil {
				return err
			}
			instruction := opts.Instruction
			if instruction == "" {
				instruction = opts.Description
			}
			resp, err := client.CreateLodestone(cmd.Context(), CreateLodestoneRequest{
				Instruction:    instruction,
				Title:          opts.Title,
				TeamNames:      opts.Teams,
				AgentProfileID: opts.ProfileID,
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

//nolint:dupl // sibling v1 commands share the same boilerplate by design
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
			if err := opts.IO.Encode(cmd.OutOrStdout(), resp); err != nil {
				return err
			}
			printV2Hint(cmd, loader, fmt.Sprintf("v2 investigations API is enabled — consider `gcx assistant investigations pause %s` (resumable) instead of cancel", args[0]))
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- list-todos ---

type todosOpts struct {
	IO cmdio.Options
}

func (o *todosOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &TodosTableCodec{})
	o.IO.RegisterCustomCodec("wide", &TodosTableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

//nolint:dupl // sibling v1 commands share the same boilerplate by design
func newTodosCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &todosOpts{}
	cmd := &cobra.Command{
		Use:   "list-todos <id>",
		Short: "List agent tasks for an investigation.",
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
			if err := opts.IO.Encode(cmd.OutOrStdout(), todos); err != nil {
				return err
			}
			printV2Hint(cmd, loader, fmt.Sprintf("v2 investigations API replaces multi-agent todos with hypotheses — try `gcx assistant investigations get %s`", args[0]))
			return nil
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

//nolint:dupl // sibling v1 commands share the same boilerplate by design
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
			timelineAgents, err := client.Timeline(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if err := opts.IO.Encode(cmd.OutOrStdout(), timelineAgents); err != nil {
				return err
			}
			printV2Hint(cmd, loader, fmt.Sprintf("v2 investigations API tracks single-agent progress via epoch + plan — try `gcx assistant investigations get %s`", args[0]))
			return nil
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

//nolint:dupl // sibling v1 commands share the same boilerplate by design
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
			if err := opts.IO.Encode(cmd.OutOrStdout(), report); err != nil {
				return err
			}
			printV2Hint(cmd, loader, fmt.Sprintf("v2 investigations API stores the report inline in session state — try `gcx assistant investigations get %s`", args[0]))
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- get-document ---

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
		Use:   "get-document <investigation-id> <document-id>",
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

// --- list-approvals ---

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
		Use:   "list-approvals <id>",
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
