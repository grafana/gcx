package errortracking

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	humanOnlyNote = "Error Tracking takes this only from a signed-in Grafana user with the Editor or Admin role, so a service account token is refused."
	signedInNote  = "Error Tracking answers this only for a signed-in Grafana user, so a service account token is refused."
	needsLogin    = " (needs a user login)"
)

func roleFor(editor bool) string {
	if editor {
		return "Editor or Admin"
	}
	return "Viewer, Editor or Admin"
}

func humanSuggestions(read string, editor bool) []string {
	signIn := "Sign in as yourself: gcx login"
	if editor {
		signIn = "Sign in as yourself, with the Editor or Admin role: gcx login"
	}
	return []string{
		signIn,
		"If GRAFANA_TOKEN is set in your environment it overrides the login: unset it for this command",
		"As a service account you can still read " + read,
	}
}

func humanOnly(err error, editor bool) error {
	status, ok := errors.AsType[*gcxerrors.HTTPStatusError](err)
	if !ok || status.Status != http.StatusForbidden {
		return err
	}
	suggestions := []string{
		"Ask an organization admin for the " + roleFor(editor) + " role in this organization",
		"Use your own login, not a service account token: gcx login",
		"If GRAFANA_TOKEN is set in your environment it overrides the login: unset it for this command",
		"Check what your login holds: gcx setup status",
	}
	return &gcxerrors.DetailedError{
		Summary:     gcxerrors.SummaryAuthorizationFailed,
		Details:     status.ServerMessage,
		Suggestions: suggestions,
		ExitCode:    new(gcxerrors.ExitAuthFailure),
	}
}

func leafName(cmd *cobra.Command) string {
	path := cmd.CommandPath()
	if _, rest, ok := strings.Cut(path, "error-tracking "); ok {
		return rest
	}
	return path
}

func readHint(cmd *cobra.Command, errID string) string {
	project := projectOf(cmd)
	if errID == "" {
		return "the errors: " + et("errors", "list", "--project", project)
	}
	return "the error: " + et("errors", "get", printable(errID), "--project", project)
}

func humanGuard(cmd *cobra.Command, what string, editor bool, errID string) func(*client) error {
	return func(c *client) error {
		if !c.robot {
			return nil
		}
		who := "a signed-in Grafana user"
		if editor {
			who += " (Editor or Admin)"
		}
		return &gcxerrors.DetailedError{
			Summary:     gcxerrors.SummaryAuthorizationFailed,
			Details:     fmt.Sprintf("%s needs %s; this context uses a service account token", what, who),
			Suggestions: humanSuggestions(readHint(cmd, errID), editor),
			ExitCode:    new(gcxerrors.ExitAuthFailure),
		}
	}
}

type errorTarget struct {
	IO      cmdio.Options
	Project projectRef
	id      int64
}

func (o *errorTarget) Validate(cmd *cobra.Command, arg string) error {
	var err error
	if o.id, err = idArg(cmd, arg); err != nil {
		return err
	}
	if err := o.Project.validate(cmd); err != nil {
		return err
	}
	return validIO(cmd, &o.IO)
}

func (o *errorTarget) connect(ctx context.Context, loader *providers.ConfigLoader, guard func(*client) error) (*client, string, error) {
	c, project, err := connect(ctx, loader, &o.Project, guard)
	if err != nil {
		return nil, "", err
	}
	return c, routePath("projects", project, "errors", o.id), nil
}

func (o *errorTarget) shown(c *client) string {
	return fmt.Sprintf("error %d in %s", o.id, c.projectShown())
}

type activityEntry struct {
	ID           string                `json:"id"`
	Type         string                `json:"type"`
	CreatedAt    string                `json:"createdAt"`
	Actor        string                `json:"actor"`
	ActorName    string                `json:"actorName,omitempty"`
	StatusChange *activityStatusChange `json:"statusChange,omitempty"`
	Comment      *activityComment      `json:"comment,omitempty"`
}

type activityStatusChange struct {
	FromStatus string         `json:"fromStatus,omitempty"`
	ToStatus   string         `json:"toStatus"`
	Release    string         `json:"release,omitempty"`
	Reason     map[string]any `json:"reason,omitempty"`
	Conditions map[string]any `json:"conditions,omitempty"`
}

type activityComment struct {
	ID       int64  `json:"id"`
	Body     string `json:"body"`
	EditedAt string `json:"editedAt,omitempty"`
	CanEdit  bool   `json:"canEdit"`
}

func (e activityEntry) what() string {
	switch {
	case e.StatusChange != nil:
		return cmdio.OrDash(e.StatusChange.FromStatus) + " → " + e.StatusChange.ToStatus
	case e.Comment != nil:
		return "comment: " + oneLine(e.Comment.Body, 80)
	}
	return e.Type
}

type errorsListActivityOpts struct {
	errorTarget

	Type    string
	Limit   int
	apiType string
}

func (o *errorsListActivityOpts) Validate(cmd *cobra.Command, arg string) error {
	if cmd.Flags().Changed("type") {
		var ok bool
		if o.apiType, ok = map[string]string{"status-change": "statusChange", "comment": "comment"}[o.Type]; !ok {
			return usage(cmd, fmt.Sprintf("--type %q: use status-change or comment", o.Type))
		}
	}
	return o.errorTarget.Validate(cmd, arg)
}

func (o *errorsListActivityOpts) setup(flags *pflag.FlagSet) {
	o.Project.setup(flags)
	flags.StringVar(&o.Type, "type", "", "Keep one kind of entry: status-change or comment")
	o.IO.BindListLimit(flags, &o.Limit, "entries", 50)
	bindListTable(&o.IO, flags, cmdio.Table[activityEntry]{Columns: []cmdio.Column[activityEntry]{
		{Header: "COMMENT ID", Content: func(e activityEntry) string {
			if e.Comment == nil {
				return "-"
			}
			return strconv.FormatInt(e.Comment.ID, 10)
		}},
		{Header: "TIME", Visible: when, Content: func(e activityEntry) string { return e.CreatedAt }},
		{Header: "ACTOR", Content: func(e activityEntry) string { return cmdio.OrDash(strings.TrimSpace(e.ActorName + " " + e.Actor)) }},
		{Header: "WHAT", Content: activityEntry.what},
	}})
}

func newErrorsListActivityCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &errorsListActivityOpts{}
	cmd := &cobra.Command{
		Use:   "list <error>",
		Short: "List an error's status changes and comments.",
		Long: `List what happened to one error (its ID from errors list), newest first:
its status changes, with who or what made each, and the comments on it.

--type keeps only status changes or only comments; a comment's ID is what
errors comments update and delete take. --limit 0 pages through every
entry; a list cut off at --limit says so in list_meta and on stderr.`,
		Example: `  # The latest activity of error 42.
  gcx error-tracking errors activity list 42 --project 314

  # Its comments only.
  gcx et errors activity list 42 --project checkout-api --type comment

  # Every status change, as JSON.
  gcx et errors activity list 42 --project 314 --type status-change --limit 0 -o json

  # As an agent: the latest comments' IDs and text.
  gcx error-tracking errors activity list 42 --project 314 --type comment --json comment.id,comment.body,createdAt`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(cmd, args[0]); err != nil {
				return err
			}
			ctx := cmd.Context()
			c, path, err := opts.connect(ctx, loader, nil)
			if err != nil {
				return err
			}
			items, hasMore, err := collectPages(ctx, opts.Limit, func(ctx context.Context, cursor string, size int) ([]activityEntry, string, error) {
				q := url.Values{"limit": {strconv.Itoa(size)}}
				if opts.apiType != "" {
					q.Set("type", opts.apiType)
				}
				if cursor != "" {
					q.Set("cursor", cursor)
				}
				var out struct {
					Activity   []activityEntry `json:"activity"`
					NextCursor string          `json:"nextCursor"`
				}
				err := c.do(ctx, http.MethodGet, path+"/activity", q, nil, &out)
				return out.Activity, out.NextCursor, err
			})
			if err != nil {
				return err
			}
			if err := encodeList(cmd, c, &opts.IO, items, cmdio.PagedListMeta(len(items), opts.Limit, hasMore, 0)); err != nil {
				return err
			}
			if len(items) == 0 && !opts.IO.JSONDiscovery {
				what := "activity"
				if opts.Type != "" {
					what = opts.Type + " entries"
				}
				say(cmd.ErrOrStderr(), fmt.Sprintf("No %s yet on %s; comment on it", what, opts.shown(c)),
					fmt.Sprintf("gcx error-tracking errors comments create %d --project %s --body <text>", opts.id, c.projectArg()))
			}
			return nil
		},
		Annotations: map[string]string{agent.AnnotationTokenCost: "medium", agent.AnnotationLLMHint: "<error> --project 314 --type comment"},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type assignee struct {
	UserUID    string `json:"userUid"`
	Name       string `json:"name"`
	Login      string `json:"login"`
	AvatarURL  string `json:"avatarUrl"`
	AssignedBy string `json:"assignedBy"`
	AssignedAt string `json:"assignedAt"`
}

type assigneeList struct {
	Items     []assignee `json:"items"`
	CanAssign bool       `json:"canAssign"`
}

func (assigneeList) ListItemsKey() string { return "items" }

type errorsListAssigneesOpts struct{ errorTarget }

func (o *errorsListAssigneesOpts) setup(flags *pflag.FlagSet) {
	o.Project.setup(flags)
	bindTable(&o.IO, flags, cmdio.Table[assignee]{Columns: []cmdio.Column[assignee]{
		{Header: "UID", Content: func(a assignee) string { return a.UserUID }},
		{Header: "NAME", Content: func(a assignee) string { return cmdio.OrDash(a.Name) }},
		{Header: "LOGIN", Content: func(a assignee) string { return cmdio.OrDash(a.Login) }},
		{Header: "ASSIGNED AT", Visible: when, Content: func(a assignee) string { return a.AssignedAt }},
	}}, func(l assigneeList) []assignee { return l.Items })
}

func newErrorsListAssigneesCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &errorsListAssigneesOpts{}
	cmd := &cobra.Command{
		Use:   "list <error>",
		Short: "List the users assigned to an error" + needsLogin + ".",
		Long: `List the Grafana users assigned to one error (its ID from errors list),
with their UIDs and when they were assigned. A user who left the organization
keeps their UID and can still be unassigned.

canAssign says whether you may change the assignees. ` + signedInNote,
		Example: `  # Who works on error 42.
  gcx error-tracking errors assignees list 42 --project 314

  # By project name, as JSON, with canAssign.
  gcx et errors assignees list 42 --project checkout-api -o json

  # As an agent: the assignees' UIDs and logins only.
  gcx error-tracking errors assignees list 42 --project 314 --json userUid,login`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(cmd, args[0]); err != nil {
				return err
			}
			c, path, err := opts.connect(cmd.Context(), loader, humanGuard(cmd, leafName(cmd), false, args[0]))
			if err != nil {
				return err
			}
			var out struct {
				Assignees []assignee `json:"assignees"`
				CanAssign bool       `json:"canAssign"`
			}
			if err := c.do(cmd.Context(), http.MethodGet, path+"/assignees", nil, nil, &out); err != nil {
				return humanOnly(err, false)
			}
			list := assigneeList{Items: out.Assignees, CanAssign: out.CanAssign}
			if list.Items == nil {
				list.Items = []assignee{}
			}
			return opts.IO.Encode(cmd.OutOrStdout(), list)
		},
		Annotations: map[string]string{agent.AnnotationTokenCost: "small", agent.AnnotationLLMHint: "<error> --project 314 --json userUid,login", agent.AnnotationRequiredRole: "Viewer"},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type errorsAssigneeOpts struct {
	errorTarget

	user string
}

func (o *errorsAssigneeOpts) setup(flags *pflag.FlagSet) {
	o.Project.setup(flags)
	bindText(&o.IO, flags, mutationText{})
}

func (o *errorsAssigneeOpts) Validate(cmd *cobra.Command, args []string) error {
	if o.user = strings.TrimSpace(args[1]); o.user == "" || o.user == "." || o.user == ".." {
		see := argHelp(cmd, "<user>", args)
		return usage(cmd, "<user> must be a user UID or me", "See them: "+see)
	}
	return o.errorTarget.Validate(cmd, args[0])
}

func (u usersItem) shown() string {
	switch {
	case u.Name != "" && u.Login != "" && u.Name != u.Login:
		return fmt.Sprintf("%s (%s)", u.Name, u.Login)
	case u.Login != "":
		return u.Login
	}
	return cmdio.OrDash(u.Name)
}

func (c *client) me(ctx context.Context) (usersItem, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.host+"/api/user", nil)
	if err != nil {
		return usersItem{}, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return usersItem{}, fmt.Errorf("error tracking: GET /api/user: %w", err)
	}
	defer resp.Body.Close()
	body := errorBody(resp)
	if resp.StatusCode/100 != 2 {
		return usersItem{}, statusError(resp, body)
	}
	var u usersItem
	if err := json.Unmarshal(body, &u); err != nil || u.UID == "" {
		return usersItem{}, errors.New("error tracking: Grafana did not say who you are (GET /api/user)")
	}
	return u, nil
}

func newErrorsAssigneeCommand(loader *providers.ConfigLoader, add bool) *cobra.Command {
	opts := &errorsAssigneeOpts{}
	cmd := &cobra.Command{
		Use:   "add <error> <user>",
		Short: "Assign a user to an error" + needsLogin + ".",
		Long: `Assign one Grafana user to one error, keeping the other assignees; a user
already assigned gives changed: false with nothing sent. <user> is a UID or me; find
a UID with users list --search <text>. ` + humanOnlyNote + ` ` + noDryRun,
		Example: `  # Assign yourself.
  gcx error-tracking errors assignees add 42 me --project 314

  # Find a teammate's UID, then assign them.
  gcx et users list --project checkout-api --search alex --json uid,login
  gcx et errors assignees add 42 u-alex --project checkout-api

  # As an agent: assign by UID and read the result.
  gcx error-tracking errors assignees add 42 u-alex --project 314 -o json`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(cmd, args); err != nil {
				return err
			}
			ctx := cmd.Context()
			c, path, err := opts.connect(ctx, loader, humanGuard(cmd, leafName(cmd), true, args[0]))
			if err != nil {
				return err
			}
			var target errorDetail
			if err := c.do(ctx, http.MethodGet, path, nil, nil, &target); err != nil {
				return humanOnly(err, true)
			}
			c.knowProject(opts.Project.id, target.ProjectName)
			user := usersItem{UID: opts.user}
			if opts.user == "me" {
				if user, err = c.me(ctx); err != nil {
					return humanOnly(err, true)
				}
			}
			var out struct {
				Assignees []assignee `json:"assignees"`
			}
			if err := c.do(ctx, http.MethodGet, path+"/assignees", nil, nil, &out); err != nil {
				return humanOnly(err, true)
			}
			current := out.Assignees
			if i := slices.IndexFunc(current, func(a assignee) bool { return a.UserUID == user.UID }); i >= 0 {
				user = usersItem{UID: current[i].UserUID, Name: current[i].Name, Login: current[i].Login}
			}
			who := user.UID
			if user.Login != "" || user.Name != "" {
				who = fmt.Sprintf("%s, UID %s", user.shown(), user.UID)
			}
			method, action, verb := http.MethodPut, "assigned", "Assigned %s to %s"
			assigned := slices.ContainsFunc(current, func(a assignee) bool { return a.UserUID == user.UID })
			if add && assigned {
				m := assignMutation{mutationBase: c.mutation(action, "Error", opts.id, false)}
				m.User = user.UID
				m.text = fmt.Sprintf("%s was already assigned to %s; nothing changed", who, opts.shown(c))
				return emit(cmd, &opts.IO, m)
			}
			if !add {
				method, action, verb = http.MethodDelete, "unassigned", "Unassigned %s from %s"
				if !assigned {
					m := assignMutation{mutationBase: c.mutation(action, "Error", opts.id, false)}
					m.User = user.UID
					m.text = fmt.Sprintf("%s was not assigned to %s; nothing changed", who, opts.shown(c))
					return emit(cmd, &opts.IO, m)
				}
			}
			if err := c.do(ctx, method, path+"/assignees/"+url.PathEscape(user.UID), nil, nil, nil); err != nil {
				return humanOnly(err, true)
			}
			m := assignMutation{mutationBase: c.mutation(action, "Error", opts.id, true)}
			m.User = user.UID
			m.text = fmt.Sprintf(verb, who, opts.shown(c))
			return emit(cmd, &opts.IO, m)
		},
		Annotations: map[string]string{agent.AnnotationTokenCost: "small", agent.AnnotationLLMHint: "<error> <user-uid|me> --project 314 -o json", agent.AnnotationRequiredRole: "Editor"},
	}
	if !add {
		cmd.Use = "delete <error> <user>"
		cmd.Short = "Unassign a user from an error" + needsLogin + "."
		cmd.Long = `Unassign one user from one error without asking; undo it with errors
assignees add. <user> is a UID or me; a UID that is not assigned gives
changed: false with nothing sent. Find a UID with users list --search <text>. ` + humanOnlyNote + ` ` + noDryRun
		cmd.Example = `  # Unassign yourself.
  gcx error-tracking errors assignees delete 42 me --project 314

  # Find a teammate's UID, then unassign them.
  gcx et users list --project checkout-api --search alex --json uid,login
  gcx et errors assignees delete 42 u-alex --project checkout-api

  # As an agent: unassign by UID and read the result.
  gcx error-tracking errors assignees delete 42 u-alex --project 314 -o json`
	}
	opts.setup(cmd.Flags())
	return cmd
}
