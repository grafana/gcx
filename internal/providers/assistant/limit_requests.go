package assistant

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/assistant"
	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
)

const limitRequestsPreamble = "This command is experimental. It may be removed, or its subcommands, " +
	"flags and responses may change without following the normal semantic versioning conventions."

// maxNoteLength matches the server's limit on a request note.
const maxNoteLength = 280

func limitRequestsCommand(loader *providers.ConfigLoader) *cobra.Command {
	group := &cobra.Command{
		Use:   "limit-requests",
		Short: "[experimental] Ask admins to raise Assistant limits, and review those requests",
		Long: limitRequestsPreamble + `

When a seat or token limit blocks you, ask your Assistant Admins to raise it,
and follow your requests with list --mine. Admins list pending requests grouped
by the limit setting that resolves them, then raise that limit, which resolves
every request it unblocks, or dismiss the requests.`,
		Annotations: map[string]string{agent.AnnotationStability: agent.StabilityExperimental},
	}
	settings := &cobra.Command{
		Use:   "settings",
		Short: "[experimental] Manage whether people can ask admins to raise limits",
		Long: limitRequestsPreamble + `

Limit requests are off for a stack until an admin turns them on.`,
		Annotations: map[string]string{agent.AnnotationStability: agent.StabilityExperimental},
	}
	settings.AddCommand(limitRequestSettingsGetCommand(loader), limitRequestSettingsUpdateCommand(loader))
	group.AddCommand(limitRequestsCreateCommand(loader), limitRequestsListCommand(loader),
		limitRequestsRaiseCommand(loader), limitRequestsDismissCommand(loader), settings)
	return group
}

type limitRequestCreateOpts struct {
	IO   cmdio.Options
	note string
}

func limitRequestsCreateCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &limitRequestCreateOpts{}
	cmd := &cobra.Command{
		Use:   "create <active_users|stack_tokens|user_tokens>",
		Short: "[experimental] Ask your Assistant Admins to raise a limit that blocks you",
		Long: limitRequestsPreamble + `

Asks your Assistant Admins to raise the limit that blocks you: active_users
(every seat is taken), stack_tokens (the stack's token limit), or user_tokens
(your per-user token limit). gcx assistant usage get names the limit to use.

The server accepts a request only while that limit blocks you and an admin can
raise it. Admins get a Grafana notification, and you get one when the limit is
raised. Asking again while a request is pending returns the same request.`,
		Example: `  gcx assistant limit-requests create active_users --note "Debugging the checkout latency alert"
  gcx assistant limit-requests create user_tokens`,
		Args:        cobra.ExactArgs(1),
		ValidArgs:   assistant.RequestableLimits,
		Annotations: map[string]string{agent.AnnotationStability: agent.StabilityExperimental},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateLimit(args[0]); err != nil {
				return err
			}
			if utf8.RuneCountInString(strings.TrimSpace(opts.note)) > maxNoteLength {
				return fmt.Errorf("--note must be at most %d characters", maxNoteLength)
			}
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, ctx, cancel, err := limitRequestsClient(cmd, loader)
			if err != nil {
				return err
			}
			defer cancel()
			created, err := assistant.CreateLimitRequest(ctx, client, args[0], opts.note)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), *created)
		},
	}
	opts.IO.RegisterCustomCodec("text", &createdLimitRequestCodec{})
	opts.IO.DefaultFormat("text")
	opts.IO.BindFlags(cmd.Flags())
	cmd.Flags().StringVar(&opts.note, "note", "", fmt.Sprintf("Why you need more, up to %d characters; your admins see it", maxNoteLength))
	return cmd
}

type limitRequestListOpts struct {
	IO    cmdio.Options
	limit string
	team  string
	mine  bool
	state string
}

// requestStates are the states list --mine filters on.
var requestStates = []string{"pending", "resolved", "dismissed", "expired"} //nolint:gochecknoglobals // fixed enum for flag validation

// limitRequestGroupView is one group and, when the caller filtered to it,
// each of its pending requests.
type limitRequestGroupView struct {
	assistant.LimitRequestGroup

	Requests []assistant.LimitRequest `json:"requests,omitempty"`
}

func limitRequestsListCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &limitRequestListOpts{}
	codec := &limitRequestListCodec{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "[experimental] List this month's pending requests to raise Assistant limits",
		Long: limitRequestsPreamble + `

Lists this month's pending limit requests, grouped by the limit setting an
admin would change: the stack's seats or token limit, one team's per-user token
grant, or the default per-user token limit. The largest group comes first.

Filter with --type or --team to also list each request in the matching groups,
oldest first, with the IDs dismiss takes. Requires the
grafana-assistant-app.usage:read permission.

With --mine, lists your own requests in every state instead, newest first, so
you can see whether admins raised the limit or dismissed the request.`,
		Example: `  gcx assistant limit-requests list
  gcx assistant limit-requests list --type user_tokens --team infra
  gcx assistant limit-requests list --type active_users -o json
  gcx assistant limit-requests list --mine --state pending`,
		Args:        cobra.NoArgs,
		Annotations: map[string]string{agent.AnnotationStability: agent.StabilityExperimental},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if opts.limit != "" {
				if err := validateLimit(opts.limit); err != nil {
					return err
				}
			}
			if opts.state != "" && !opts.mine {
				return errors.New("--state needs --mine: admins' lists hold only pending requests")
			}
			if opts.mine && (opts.limit != "" || opts.team != "") {
				return errors.New("--mine lists all your requests; it can't be combined with --type or --team")
			}
			if opts.state != "" && !slices.Contains(requestStates, opts.state) {
				return fmt.Errorf("unknown state %q: use one of %s", opts.state, strings.Join(requestStates, ", "))
			}
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, ctx, cancel, err := limitRequestsClient(cmd, loader)
			if err != nil {
				return err
			}
			defer cancel()
			if opts.mine {
				mine, err := assistant.ListMyLimitRequests(ctx, client, opts.state)
				if err != nil {
					return err
				}
				return opts.IO.Encode(cmd.OutOrStdout(), mine.Requests)
			}
			groups, err := listLimitRequestGroups(ctx, client, opts.limit, opts.team)
			if err != nil {
				return err
			}
			codec.expand = opts.limit != "" || opts.team != ""
			views := make([]limitRequestGroupView, 0, len(groups))
			for _, group := range groups {
				view := limitRequestGroupView{LimitRequestGroup: group}
				if codec.expand {
					if view.Requests, err = assistant.ListGroupRequests(ctx, client, group.ID); err != nil {
						return err
					}
				}
				views = append(views, view)
			}
			return opts.IO.Encode(cmd.OutOrStdout(), views)
		},
	}
	opts.IO.RegisterCustomCodec("text", codec)
	opts.IO.DefaultFormat("text")
	opts.IO.BindFlags(cmd.Flags())
	cmd.Flags().StringVar(&opts.limit, "type", "", "Only requests for this limit: "+strings.Join(assistant.RequestableLimits, ", "))
	cmd.Flags().StringVar(&opts.team, "team", "", "Only requests for this team's per-user token grant, by team name or UID")
	cmd.Flags().BoolVar(&opts.mine, "mine", false, "List your own requests instead of the stack's pending ones")
	cmd.Flags().StringVar(&opts.state, "state", "", "With --mine, only requests in this state: "+strings.Join(requestStates, ", "))
	return cmd
}

// listLimitRequestGroups lists groups and names their teams. A team filter
// matches a UID or, case-insensitively, a name.
func listLimitRequestGroups(ctx context.Context, client *assistanthttp.Client, limit, team string) ([]assistant.LimitRequestGroup, error) {
	groups, err := assistant.ListLimitRequestGroups(ctx, client, limit, "")
	if err != nil {
		return nil, err
	}
	if slices.ContainsFunc(groups, func(g assistant.LimitRequestGroup) bool { return g.Scope.Kind == "team" }) {
		// Team names come from the usage snapshot, which needs the same
		// usage:read permission as the groups themselves.
		if usage, err := assistant.FetchAdminUsage(ctx, client); err == nil {
			assistant.NameTeams(groups, usage.Teams)
		} else if _, denied := errors.AsType[*assistanthttp.ForbiddenError](err); !denied {
			return nil, err
		}
	}
	if team != "" {
		groups = slices.DeleteFunc(groups, func(g assistant.LimitRequestGroup) bool {
			return g.Scope.Kind != "team" || (g.Scope.TeamUID != team && !strings.EqualFold(g.TeamName, team))
		})
	}
	return groups, nil
}

func validateLimit(limit string) error {
	if !slices.Contains(assistant.RequestableLimits, limit) {
		return fmt.Errorf("unknown limit %q: use one of %s", limit, strings.Join(assistant.RequestableLimits, ", "))
	}
	return nil
}

func limitRequestsClient(cmd *cobra.Command, loader *providers.ConfigLoader) (*assistanthttp.Client, context.Context, context.CancelFunc, error) {
	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
	cfg, err := loader.LoadGrafanaConfig(ctx)
	if err != nil {
		cancel()
		return nil, nil, nil, err
	}
	client, err := assistanthttp.NewClient(cfg)
	if err != nil {
		cancel()
		return nil, nil, nil, err
	}
	return client, ctx, cancel, nil
}

type createdLimitRequestCodec struct{}

func (c *createdLimitRequestCodec) Format() format.Format { return "text" }

func (c *createdLimitRequestCodec) Encode(dst io.Writer, value any) error {
	created, ok := value.(assistant.CreatedLimitRequest)
	if !ok {
		return fmt.Errorf("expected CreatedLimitRequest, got %T", value)
	}
	if created.Created {
		cmdio.Success(dst, "Request sent to your Assistant Admins.")
	} else {
		cmdio.Success(dst, "You already asked on %s; your Assistant Admins have your request.", created.Request.CreatedAt.UTC().Format("Jan 2"))
	}
	_, err := fmt.Fprintln(dst, "  You'll get a Grafana notification when they respond.")
	return err
}

func (c *createdLimitRequestCodec) Decode(io.Reader, any) error {
	return errors.New("decode not supported for text format")
}

// limitRequestListCodec shows the groups, and each request when the caller
// filtered the list.
type limitRequestListCodec struct {
	expand bool
}

func (c *limitRequestListCodec) Format() format.Format { return "text" }

func (c *limitRequestListCodec) Encode(dst io.Writer, value any) error {
	if mine, ok := value.([]assistant.LimitRequest); ok {
		return encodeMyLimitRequests(dst, mine)
	}
	views, ok := value.([]limitRequestGroupView)
	if !ok {
		return fmt.Errorf("expected []limitRequestGroupView, got %T", value)
	}
	if len(views) == 0 {
		_, err := fmt.Fprintln(dst, "No pending limit requests.")
		return err
	}
	if c.expand {
		// Each group's requests are already oldest first; merge them by time.
		type row struct {
			request assistant.LimitRequest
			scope   string
		}
		var rows []row
		for _, view := range views {
			for _, r := range view.Requests {
				rows = append(rows, row{r, view.ScopeLabel()})
			}
		}
		slices.SortStableFunc(rows, func(a, b row) int { return a.request.CreatedAt.Compare(b.request.CreatedAt) })
		table := style.NewTable("ID", "REQUESTER", "LIMIT", "SCOPE", "REQUESTED", "NOTE").Compact()
		for _, r := range rows {
			table.Row(r.request.ID, cmdio.OrDash(r.request.Requester), assistant.LimitLabel(r.request.LimitKey), r.scope, r.request.CreatedAt.UTC().Format("Jan 2 15:04"), cmdio.OrDash(r.request.Note))
		}
		return table.Render(dst)
	}
	groups := make([]assistant.LimitRequestGroup, 0, len(views))
	for _, view := range views {
		groups = append(groups, view.LimitRequestGroup)
	}
	if err := assistant.WriteLimitRequestGroups(dst, groups); err != nil {
		return err
	}
	hint := "gcx assistant limit-requests list --type " + groups[0].LimitKey
	if groups[0].Scope.Kind == "team" {
		hint += " --team " + cmdio.ShellCommand([]string{cmp.Or(groups[0].TeamName, groups[0].Scope.TeamUID)})
	}
	_, err := fmt.Fprintln(dst, style.ColorMutedText("See each request: "+hint))
	return err
}

func (c *limitRequestListCodec) Decode(io.Reader, any) error {
	return errors.New("decode not supported for text format")
}

// textCodec renders a command's result as text with fn.
type textCodec func(dst io.Writer, value any) error

func (c textCodec) Format() format.Format                 { return "text" }
func (c textCodec) Encode(dst io.Writer, value any) error { return c(dst, value) }
func (c textCodec) Decode(io.Reader, any) error {
	return errors.New("decode not supported for text format")
}

type limitRaiseOpts struct {
	IO     cmdio.Options
	to     string
	team   string
	dryRun bool
}

// limitRaise is the outcome of raising the limit a group of requests asks about.
type limitRaise struct {
	Group assistant.LimitRequestGroup `json:"group"`
	// Previous is the replaced limit; nil means the stack used its default.
	Previous *int64 `json:"previous"`
	Value    int64  `json:"value"`
	Resolves int    `json:"resolves"`
	DryRun   bool   `json:"dryRun"`
}

func limitRequestsRaiseCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &limitRaiseOpts{}
	cmd := &cobra.Command{
		Use:   "raise <active_users|stack_tokens|user_tokens> --to <value>",
		Short: "[experimental] Raise the limit that pending requests ask about",
		Long: limitRequestsPreamble + `

Raises the limit setting behind a group of pending requests: the stack's seats
(active_users), the stack's token limit (stack_tokens), or a per-user token
limit (user_tokens), which is the default per-user limit or, with --team, that
team's per-user grant. The server resolves every pending request the new value
unblocks and notifies the requesters.

--to is seats, or tokens in whole millions such as 5M. --dry-run reads the
current limit and reports how many requests the value would resolve without
changing it. Requires the grafana-assistant-app.usage:write permission.`,
		Example: `  gcx assistant limit-requests raise active_users --to 15 --dry-run
  gcx assistant limit-requests raise user_tokens --team infra --to 20M
  gcx assistant limit-requests raise stack_tokens --to 500M -o json`,
		Args:        cobra.ExactArgs(1),
		ValidArgs:   assistant.RequestableLimits,
		Annotations: map[string]string{agent.AnnotationStability: agent.StabilityExperimental},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateLimit(args[0]); err != nil {
				return err
			}
			if opts.team != "" && args[0] != assistant.LimitUserTokens {
				return errors.New("--team applies only to user_tokens")
			}
			value, err := parseLimitValue(opts.to)
			if err != nil {
				return err
			}
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, ctx, cancel, err := limitRequestsClient(cmd, loader)
			if err != nil {
				return err
			}
			defer cancel()
			group, err := findLimitRequestGroup(ctx, client, args[0], opts.team)
			if err != nil {
				return err
			}
			result := limitRaise{Group: *group, Value: value, DryRun: opts.dryRun}
			// Preview before writing: afterwards the requests are resolved.
			if result.Resolves, err = assistant.PreviewRaise(ctx, client, group.ID, value); err != nil {
				return err
			}
			if result.Previous, err = assistant.RaiseLimit(ctx, client, *group, value, !opts.dryRun); err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}
	opts.IO.RegisterCustomCodec("text", textCodec(encodeLimitRaise))
	opts.IO.DefaultFormat("text")
	opts.IO.BindFlags(cmd.Flags())
	cmd.Flags().StringVar(&opts.to, "to", "", "The new limit: seats, or tokens in whole millions such as 5M")
	cmd.Flags().StringVar(&opts.team, "team", "", "With user_tokens, raise this team's per-user grant, by team name or UID")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "Report what the raise would resolve without changing the limit")
	_ = cmd.MarkFlagRequired("to")
	return cmd
}

// parseLimitValue reads a positive count with an optional K, M, or B suffix.
func parseLimitValue(raw string) (int64, error) {
	digits, scale := strings.TrimSpace(raw), int64(1)
	if digits != "" {
		switch strings.ToUpper(digits[len(digits)-1:]) {
		case "K":
			scale = 1_000
		case "M":
			scale = 1_000_000
		case "B":
			scale = 1_000_000_000
		}
	}
	if scale > 1 {
		digits = digits[:len(digits)-1]
	}
	n, err := strconv.ParseInt(digits, 10, 64)
	if err != nil || n <= 0 || n > math.MaxInt64/scale {
		return 0, fmt.Errorf("--to must be a positive number such as 15 or 5M, not %q", raw)
	}
	return n * scale, nil
}

// findLimitRequestGroup finds the group of pending requests for one limit
// setting. user_tokens without a team is the default per-user limit.
func findLimitRequestGroup(ctx context.Context, client *assistanthttp.Client, limit, team string) (*assistant.LimitRequestGroup, error) {
	groups, err := listLimitRequestGroups(ctx, client, limit, team)
	if err != nil {
		return nil, err
	}
	if limit == assistant.LimitUserTokens && team == "" {
		groups = slices.DeleteFunc(groups, func(g assistant.LimitRequestGroup) bool { return g.Scope.Kind == "team" })
	}
	switch len(groups) {
	case 0:
		what := assistant.LimitLabel(limit)
		if team != "" {
			what += " for team " + team
		}
		return nil, fmt.Errorf("no pending requests for %s this month; see them all with gcx assistant limit-requests list", what)
	case 1:
		return &groups[0], nil
	}
	return nil, fmt.Errorf("%d groups of pending requests match %s; narrow it with --team", len(groups), limit)
}

func encodeLimitRaise(dst io.Writer, value any) error {
	raise, ok := value.(limitRaise)
	if !ok {
		return fmt.Errorf("expected limitRaise, got %T", value)
	}
	unit := "tokens"
	if raise.Group.LimitKey == assistant.LimitActiveUsers {
		unit = "seats"
	}
	from := "its default"
	if raise.Previous != nil {
		from = formatLimit(*raise.Previous)
	}
	change := fmt.Sprintf("%s (%s) from %s to %s %s", assistant.LimitLabel(raise.Group.LimitKey), raise.Group.ScopeLabel(), from, formatLimit(raise.Value), unit)
	resolves := fmt.Sprintf("%d of %d pending %s", raise.Resolves, raise.Group.PendingCount, plural(raise.Group.PendingCount, "request"))
	if raise.DryRun {
		cmdio.Info(dst, "Would raise %s.", change)
		_, err := fmt.Fprintf(dst, "  That resolves %s. Run again without --dry-run to apply it.\n", resolves)
		return err
	}
	cmdio.Success(dst, "Raised %s.", change)
	_, err := fmt.Fprintf(dst, "  That resolves %s; Grafana notifies each requester.\n", resolves)
	return err
}

func formatLimit(v int64) string {
	if v >= 1_000_000 && v%1_000_000 == 0 {
		return strconv.FormatInt(v/1_000_000, 10) + "M"
	}
	return strconv.FormatInt(v, 10)
}

func plural(n int, noun string) string {
	if n == 1 {
		return noun
	}
	return noun + "s"
}

type limitDismiss struct {
	Dismissed int `json:"dismissed"`
}

type limitDismissOpts struct {
	IO    cmdio.Options
	limit string
	team  string
	force bool
}

func limitRequestsDismissCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &limitDismissOpts{}
	cmd := &cobra.Command{
		Use:   "dismiss [request-id]",
		Short: "[experimental] Decline pending requests without changing a limit",
		Long: limitRequestsPreamble + `

Dismisses one pending request by ID, or with --type (and --team for a team's
per-user grant) every pending request in that group. The limit is unchanged and
Grafana notifies each requester. list --type shows request IDs. Prompts for
confirmation unless --force is set. Requires the
grafana-assistant-app.usage:write permission.`,
		Example: `  gcx assistant limit-requests dismiss 0b6f3c8e-2d4a-4c1e-9f7a-1e2d3c4b5a69
  gcx assistant limit-requests dismiss --type user_tokens --team infra --force`,
		Args:        cobra.MaximumNArgs(1),
		Annotations: map[string]string{agent.AnnotationStability: agent.StabilityExperimental},
		RunE: func(cmd *cobra.Command, args []string) error {
			if (len(args) == 1) == (opts.limit != "") {
				return errors.New("pass a request ID or --type, not both")
			}
			if opts.team != "" && opts.limit != assistant.LimitUserTokens {
				return errors.New("--team applies only to --type user_tokens")
			}
			if opts.limit != "" {
				if err := validateLimit(opts.limit); err != nil {
					return err
				}
			}
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, ctx, cancel, err := limitRequestsClient(cmd, loader)
			if err != nil {
				return err
			}
			defer cancel()
			prompt := fmt.Sprintf("Dismiss limit request %s?", cmp.Or(args...))
			var group *assistant.LimitRequestGroup
			if opts.limit != "" {
				if group, err = findLimitRequestGroup(ctx, client, opts.limit, opts.team); err != nil {
					return err
				}
				prompt = fmt.Sprintf("Dismiss %d pending %s for %s (%s)?", group.PendingCount, plural(group.PendingCount, "request"), assistant.LimitLabel(group.LimitKey), group.ScopeLabel())
			}
			proceed, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.ErrOrStderr(), opts.force, prompt)
			if err != nil {
				return err
			}
			if !proceed {
				return gcxerrors.DetailedError{Summary: "dismiss cancelled", ExitCode: new(gcxerrors.ExitCancelled)}
			}
			result := limitDismiss{Dismissed: 1}
			if group != nil {
				result.Dismissed, err = assistant.DismissLimitRequestGroup(ctx, client, group.ID)
			} else {
				_, err = assistant.DismissLimitRequest(ctx, client, args[0])
			}
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}
	opts.IO.RegisterCustomCodec("text", textCodec(func(dst io.Writer, value any) error {
		dismissed, ok := value.(limitDismiss)
		if !ok {
			return fmt.Errorf("expected limitDismiss, got %T", value)
		}
		cmdio.Success(dst, "Dismissed %d %s. Grafana notifies each requester.", dismissed.Dismissed, plural(dismissed.Dismissed, "request"))
		return nil
	}))
	opts.IO.DefaultFormat("text")
	opts.IO.BindFlags(cmd.Flags())
	cmd.Flags().StringVar(&opts.limit, "type", "", "Dismiss every pending request for this limit: "+strings.Join(assistant.RequestableLimits, ", "))
	cmd.Flags().StringVar(&opts.team, "team", "", "With --type user_tokens, the team whose per-user grant the requests ask about")
	cmd.Flags().BoolVar(&opts.force, "force", false, "Skip the confirmation prompt")
	return cmd
}

func limitRequestSettingsGetCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &struct{ IO cmdio.Options }{}
	cmd := &cobra.Command{
		Use:   "get",
		Short: "[experimental] Show whether people can ask admins to raise limits",
		Long: limitRequestsPreamble + `

Shows whether this stack accepts limit requests (off by default), how admins
hear about them, and the usage percentage at which people are warned that a
limit is close. Requires the grafana-assistant-app.usage:read permission.`,
		Example:     `  gcx assistant limit-requests settings get -o json`,
		Args:        cobra.NoArgs,
		Annotations: map[string]string{agent.AnnotationStability: agent.StabilityExperimental},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, ctx, cancel, err := limitRequestsClient(cmd, loader)
			if err != nil {
				return err
			}
			defer cancel()
			settings, err := assistant.GetLimitRequestSettings(ctx, client)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), *settings)
		},
	}
	opts.IO.RegisterCustomCodec("text", textCodec(encodeLimitRequestSettings))
	opts.IO.DefaultFormat("text")
	opts.IO.BindFlags(cmd.Flags())
	return cmd
}

type limitRequestSettingsUpdateOpts struct {
	IO                                          cmdio.Options
	enabled, inbox, emailDigest, alert, alertEm bool
	threshold                                   int
}

func limitRequestSettingsUpdateCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &limitRequestSettingsUpdateOpts{}
	cmd := &cobra.Command{
		Use:   "update",
		Short: "[experimental] Turn limit requests on or off and change their notifications",
		Long: limitRequestsPreamble + `

Changes only the settings whose flags are set and keeps the rest. Requires the
grafana-assistant-app.usage:write permission.`,
		Example: `  gcx assistant limit-requests settings update --enabled
  gcx assistant limit-requests settings update --alert-threshold 90 --admin-email-digest=false`,
		Args:        cobra.NoArgs,
		Annotations: map[string]string{agent.AnnotationStability: agent.StabilityExperimental},
		RunE: func(cmd *cobra.Command, _ []string) error {
			flags := cmd.Flags()
			names := []string{"enabled", "admin-inbox", "admin-email-digest", "alert", "alert-email", "alert-threshold"}
			if !slices.ContainsFunc(names, flags.Changed) {
				return errors.New("set at least one of --" + strings.Join(names, ", --"))
			}
			if flags.Changed("alert-threshold") && (opts.threshold < 1 || opts.threshold > 99) {
				return errors.New("--alert-threshold must be between 1 and 99")
			}
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, ctx, cancel, err := limitRequestsClient(cmd, loader)
			if err != nil {
				return err
			}
			defer cancel()
			// PUT replaces every setting, so start from the saved ones.
			settings, err := assistant.GetLimitRequestSettings(ctx, client)
			if err != nil {
				return err
			}
			for name, dst := range map[string]*bool{
				"enabled": &settings.Enabled, "admin-inbox": &settings.AdminChannels.Inbox,
				"admin-email-digest": &settings.AdminChannels.EmailDigest, "alert": &settings.Alert.Enabled,
				"alert-email": &settings.Alert.Email,
			} {
				if flags.Changed(name) {
					*dst, _ = flags.GetBool(name)
				}
			}
			if flags.Changed("alert-threshold") {
				settings.Alert.ThresholdPercent = opts.threshold
			}
			if settings, err = assistant.UpdateLimitRequestSettings(ctx, client, *settings); err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), *settings)
		},
	}
	opts.IO.RegisterCustomCodec("text", textCodec(encodeLimitRequestSettings))
	opts.IO.DefaultFormat("text")
	opts.IO.BindFlags(cmd.Flags())
	cmd.Flags().BoolVar(&opts.enabled, "enabled", false, "Let people ask Assistant Admins to raise a limit that blocks them")
	cmd.Flags().BoolVar(&opts.inbox, "admin-inbox", false, "Notify admins of new requests in their Grafana inbox")
	cmd.Flags().BoolVar(&opts.emailDigest, "admin-email-digest", false, "Email admins a digest of new requests")
	cmd.Flags().BoolVar(&opts.alert, "alert", false, "Warn people when their usage nears a limit")
	cmd.Flags().BoolVar(&opts.alertEm, "alert-email", false, "Also send that warning by email")
	cmd.Flags().IntVar(&opts.threshold, "alert-threshold", 0, "Usage percentage, 1 to 99, that triggers the warning")
	return cmd
}

func encodeLimitRequestSettings(dst io.Writer, value any) error {
	s, ok := value.(assistant.LimitRequestSettings)
	if !ok {
		return fmt.Errorf("expected LimitRequestSettings, got %T", value)
	}
	onOff := func(on bool) string {
		if on {
			return "on"
		}
		return "off"
	}
	table := style.NewTable("SETTING", "VALUE").Compact()
	table.Row("Limit requests", onOff(s.Enabled))
	table.Row("Admin inbox", onOff(s.AdminChannels.Inbox))
	table.Row("Admin email digest", onOff(s.AdminChannels.EmailDigest))
	table.Row("Usage alert", onOff(s.Alert.Enabled))
	table.Row("Usage alert at", strconv.Itoa(s.Alert.ThresholdPercent)+"%")
	table.Row("Usage alert email", onOff(s.Alert.Email))
	return table.Render(dst)
}

// encodeMyLimitRequests lists the caller's own requests, newest first.
func encodeMyLimitRequests(dst io.Writer, requests []assistant.LimitRequest) error {
	if len(requests) == 0 {
		_, err := fmt.Fprintln(dst, "You have no limit requests.")
		return err
	}
	table := style.NewTable("LIMIT", "STATE", "REQUESTED", "ENDED", "NOTE").Compact()
	for _, r := range requests {
		state := r.State
		if r.Resolution != "" && r.Resolution != r.State {
			state += " (" + strings.ReplaceAll(r.Resolution, "_", " ") + ")"
		}
		ended := "-"
		if r.ResolvedAt != nil {
			ended = r.ResolvedAt.UTC().Format("Jan 2 15:04")
		}
		table.Row(assistant.LimitLabel(r.LimitKey), state, r.CreatedAt.UTC().Format("Jan 2 15:04"), ended, cmdio.OrDash(r.Note))
	}
	return table.Render(dst)
}

// explainLimit follows a prompt a monthly limit refused with what blocks the
// caller and how to ask for more. It is best effort: the prompt already
// failed, so a failure here only drops the explanation.
func explainLimit(ctx context.Context, w io.Writer, loader *providers.ConfigLoader) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cfg, err := loader.LoadGrafanaConfig(ctx)
	if err != nil {
		return
	}
	client, err := assistanthttp.NewClient(cfg)
	if err != nil {
		return
	}
	usage, err := assistant.FetchUsage(ctx, client)
	if err != nil || assistant.FetchLimitContext(ctx, client, usage) != nil {
		return
	}
	if assistant.WriteLimitsReached(w, *usage) {
		return
	}
	// Seat counts are admin-only, so a seat block shows only as the refusal.
	fmt.Fprintln(w, style.ColorMutedText("See your limits: gcx assistant usage get"))
	if mine := usage.MyLimitRequests; mine != nil && mine.RequestsEnabled {
		fmt.Fprintln(w, style.ColorMutedText("If every seat is taken, ask your admins:")+" gcx assistant limit-requests create active_users --note \"<why>\"")
	}
}
