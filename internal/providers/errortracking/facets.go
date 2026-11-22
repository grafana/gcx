package errortracking

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/agent"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type environmentsItem struct {
	Environment string `json:"environment"`
	FirstSeen   string `json:"firstSeen"`
	LastSeen    string `json:"lastSeen"`
	EventCount  int64  `json:"eventCount"`
}

type transactionsItem struct {
	Transaction string `json:"transaction"`
	ErrorCount  int64  `json:"errorCount"`
	EventCount  int64  `json:"eventCount"`
}

type fieldsKey struct {
	Key        string `json:"key"`
	ValueCount int64  `json:"valueCount"`
	ErrorCount int64  `json:"errorCount"`
	EventCount int64  `json:"eventCount"`
}

type fieldsValue struct {
	Value      string `json:"value"`
	ErrorCount int64  `json:"errorCount"`
	EventCount int64  `json:"eventCount"`
	FirstSeen  string `json:"firstSeen"`
	LastSeen   string `json:"lastSeen"`
}

func newEnvironmentsListCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &facetsOpts[environmentsItem]{window: true, table: seenTable("ENVIRONMENT", func(e environmentsItem) seen {
		return seen{e.Environment, e.EventCount, e.FirstSeen, e.LastSeen}
	})}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the environments an Error Tracking project saw events from.",
		Long: `List the environments an Error Tracking project received events from, as the
SDK named them, with lifetime event counts and first and last event times.

With --since or --from/--to, only environments seen in that window are
listed; their counts stay lifetime counts. The API returns at most 100.`,
		Example: `  # Every environment.
  gcx error-tracking environments list --project 314

  # Environments that sent events today.
  gcx et environments list --project checkout-api --since 24h

  # As an agent: environment names and event counts only.
  gcx error-tracking environments list --project 314 --json environment,eventCount`,
		Annotations: map[string]string{agent.AnnotationTokenCost: "medium", agent.AnnotationLLMHint: "--project 314 --json environment,eventCount"},
	}
	return opts.sdkFacet(cmd, loader, "environment")
}

func newTransactionsListCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &facetsOpts[transactionsItem]{table: cmdio.Table[transactionsItem]{Columns: []cmdio.Column[transactionsItem]{
		{Header: "TRANSACTION", Content: func(t transactionsItem) string { return t.Transaction }},
		{Header: "ERRORS", Content: func(t transactionsItem) string { return strconv.FormatInt(t.ErrorCount, 10) }},
		{Header: "EVENTS", Content: func(t transactionsItem) string { return strconv.FormatInt(t.EventCount, 10) }},
	}}}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the transactions an Error Tracking project's errors happened in.",
		Long: `List the transactions (routes, jobs or screens, as the SDK named them) an
Error Tracking project's errors were last seen in, with error and event
counts.

The API returns the 100 with the most events, most first.`,
		Example: `  # Busiest transactions.
  gcx error-tracking transactions list --project 314

  # Names and event counts only.
  gcx et transactions list --project 314 --json transaction,eventCount

  # Every transaction, by project name, as JSON.
  gcx et transactions list --project checkout-api -o json

  # As an agent: the busiest transactions with their error counts.
  gcx error-tracking transactions list --project 314 --json transaction,errorCount,eventCount`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(cmd); err != nil {
				return err
			}
			opts.empty = opts.none("transactions", "an error carries one when the SDK names the route, job or screen")
			return opts.list(cmd, loader, "transactions", "transactions", url.Values{})
		},
		Annotations: map[string]string{agent.AnnotationTokenCost: "medium", agent.AnnotationLLMHint: "--project 314 --json transaction,eventCount"},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func newFieldsListCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &facetsOpts[fieldsKey]{window: true, table: cmdio.Table[fieldsKey]{Columns: []cmdio.Column[fieldsKey]{
		{Header: "KEY", Content: func(k fieldsKey) string { return k.Key }},
		{Header: "VALUES", Content: func(k fieldsKey) string { return strconv.FormatInt(k.ValueCount, 10) }},
		{Header: "ERRORS", Content: func(k fieldsKey) string { return strconv.FormatInt(k.ErrorCount, 10) }},
		{Header: "EVENTS", Content: func(k fieldsKey) string { return strconv.FormatInt(k.EventCount, 10) }},
	}}}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the custom field keys an Error Tracking project's events carry.",
		Long: `List the custom field keys (SDK tags) an Error Tracking project's events
carry, with how many distinct values, errors and events each has.

With --since or --from/--to, only keys seen in that window are listed. The
API returns at most 100, most events first; use fields list-values to see one
key's values. A key is what --custom-field takes.`,
		Example: `  # Keys seen in the last day.
  gcx error-tracking fields list --project 314 --since 24h

  # Key names only.
  gcx et fields list --project 314 --json key

  # As an agent: keys and their event counts only.
  gcx error-tracking fields list --project 314 --since 24h --json key,eventCount`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(cmd); err != nil {
				return err
			}
			opts.empty = opts.none("custom fields", "an event carries them when the SDK sets tags or contexts")
			return opts.list(cmd, loader, "customFields", "keys", url.Values{})
		},
		Annotations: map[string]string{agent.AnnotationTokenCost: "medium", agent.AnnotationLLMHint: "--project 314 --since 24h"},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type fieldsListValuesOpts struct {
	facetsOpts[fieldsValue]

	Key    string
	Prefix string
}

func (o *fieldsListValuesOpts) setup(flags *pflag.FlagSet) {
	flags.StringVar(&o.Prefix, "prefix", "", "Keep values that start with this text (case-sensitive)")
	o.facetsOpts.setup(flags)
}

func (o *fieldsListValuesOpts) Validate(cmd *cobra.Command) error {
	o.Key = strings.TrimSpace(o.Key)
	if o.Key == "" {
		return usage(cmd, "<key> must not be empty")
	}
	if err := nonEmpty(cmd, "prefix"); err != nil {
		return err
	}
	return o.facetsOpts.Validate(cmd)
}

func newFieldsListValuesCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &fieldsListValuesOpts{facetsOpts: facetsOpts[fieldsValue]{window: true, table: cmdio.Table[fieldsValue]{Columns: []cmdio.Column[fieldsValue]{
		{Header: "VALUE", Content: func(v fieldsValue) string { return v.Value }},
		{Header: "ERRORS", Content: func(v fieldsValue) string { return strconv.FormatInt(v.ErrorCount, 10) }},
		{Header: "EVENTS", Content: func(v fieldsValue) string { return strconv.FormatInt(v.EventCount, 10) }},
		{Header: "FIRST SEEN", Visible: when, Content: func(v fieldsValue) string { return v.FirstSeen }},
		{Header: "LAST SEEN", Visible: when, Content: func(v fieldsValue) string { return v.LastSeen }},
	}}}}
	cmd := &cobra.Command{
		Use:   "list-values <key>",
		Short: "List the values of one custom field key in an Error Tracking project.",
		Long: `List the values one custom field key from fields list has in an Error
Tracking project's events, with error and event counts and first and last
event times. The API returns at most 100, most events first, and an unknown
key gives an empty list that stderr explains.`,
		Example: `  # Every value of the browser tag.
  gcx error-tracking fields list-values browser --project 314

  # Values that start with "Chrome".
  gcx et fields list-values browser --project 314 --prefix Chrome

  # As an agent: values and event counts only.
  gcx error-tracking fields list-values browser --project 314 --json value,eventCount`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Key = args[0]
			if err := opts.Validate(cmd); err != nil {
				return err
			}
			q := url.Values{"key": {opts.Key}}
			if opts.Prefix != "" {
				q.Set("prefix", opts.Prefix)
			}
			opts.empty = func(cmd *cobra.Command, c *client, project int64) {
				what := fmt.Sprintf("%s has no custom field %q", capital(c.projectShown()), printable(opts.Key))
				if opts.Prefix != "" || opts.Time.from != "" || opts.Time.to != "" {
					what = fmt.Sprintf("No values of custom field %q match in %s", printable(opts.Key), c.projectShown())
				}
				say(cmd.ErrOrStderr(), what+"; its keys", fmt.Sprintf("gcx error-tracking fields list --project %d", project))
			}
			return badRequest(opts.list(cmd, loader, "customFields/values", "values", q), "See the keys: "+et("fields", "list", "--project", projectOf(cmd)))
		},
		Annotations: map[string]string{agent.AnnotationTokenCost: "medium", agent.AnnotationLLMHint: "<key> --project 314 --prefix <text>"},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type usersItem struct {
	UID       string `json:"uid"`
	Name      string `json:"name"`
	Login     string `json:"login"`
	AvatarURL string `json:"avatarUrl"`
}

type usersListOpts struct {
	IO      cmdio.Options
	Project projectRef
	Query   string
	Limit   int
}

func (o *usersListOpts) setup(flags *pflag.FlagSet) {
	o.Project.setup(flags)
	flags.StringVar(&o.Query, "search", "", "Text to search users by; matching follows the server's rules; errors assignees add and delete take the UID it shows")
	flags.IntVar(&o.Limit, "limit", 50, fmt.Sprintf("Maximum number of users to show, 1 to %d", userPageSize))
	bindListTable(&o.IO, flags, cmdio.Table[usersItem]{Columns: []cmdio.Column[usersItem]{
		{Header: "UID", Content: func(u usersItem) string { return u.UID }},
		{Header: "NAME", Content: func(u usersItem) string { return cmdio.OrDash(u.Name) }},
		{Header: "LOGIN", Content: func(u usersItem) string { return cmdio.OrDash(u.Login) }},
	}})
}

func (o *usersListOpts) Validate(cmd *cobra.Command) error {
	if err := o.Project.validate(cmd); err != nil {
		return err
	}
	if err := nonEmpty(cmd, "search"); err != nil {
		return err
	}
	if o.Limit < 1 || o.Limit > userPageSize {
		return usage(cmd, fmt.Sprintf("invalid --limit %d: must be 1 to %d", o.Limit, userPageSize))
	}
	return validIO(cmd, &o.IO)
}

const userPageSize = 100

func (c *client) users(ctx context.Context, id int64, query string, size int) ([]usersItem, *int, error) {
	q := url.Values{"limit": {strconv.Itoa(size)}, "page": {"1"}}
	if query != "" {
		q.Set("query", query)
	}
	var out struct {
		Users []usersItem `json:"users"`
		Total *int        `json:"totalCount"`
	}
	err := c.do(ctx, http.MethodGet, routePath("projects", id, "users"), q, nil, &out)
	return out.Users, out.Total, err
}

func newUsersListCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &usersListOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the Grafana users of an Error Tracking project" + needsLogin + ".",
		Long: `List the stack's Grafana users who can use an Error Tracking project, by
name, with the UID errors assignees add takes. It reads one page of at most
100 users; narrow it with --search. A list cut off at --limit says so in
list_meta and on stderr with a command that shows more. ` + signedInNote,
		Example: `  # The first 50 users.
  gcx error-tracking users list --project 314

  # Find a user's UID.
  gcx et users list --project 314 --search alex --json uid,login

  # As an agent: up to 100 users' UIDs and logins.
  gcx error-tracking users list --project 314 --limit 100 --json uid,login`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(cmd); err != nil {
				return err
			}
			ctx := cmd.Context()
			c, id, err := connect(ctx, loader, &opts.Project, humanGuard(cmd, leafName(cmd), false, ""))
			if err != nil {
				return err
			}
			users, total, err := c.users(ctx, id, strings.TrimSpace(opts.Query), min(opts.Limit+1, userPageSize))
			if err != nil {
				return humanOnly(err, false)
			}
			full := len(users) == opts.Limit && ((total == nil && opts.Limit == userPageSize) || (total != nil && *total > opts.Limit))
			more := len(users) > opts.Limit || full
			users = users[:min(len(users), opts.Limit)]
			meta := cmdio.PagedListMeta(len(users), opts.Limit, more, userPageSize)
			if meta != nil && meta.Cap == 0 && 2*len(users) > userPageSize {
				meta.Continue = cmdio.BuildListLimitCommand(c.project.argv(), userPageSize)
			}
			if err := encodeList(cmd, c, &opts.IO, users, meta); err != nil || len(users) > 0 || opts.IO.JSONDiscovery {
				return err
			}
			if query := strings.TrimSpace(opts.Query); query != "" {
				say(cmd.ErrOrStderr(), fmt.Sprintf("No user of %s matches the search %q; see them all", c.projectShown(), printable(query)),
					et("users", "list", "--project", strconv.FormatInt(id, 10)))
				return nil
			}
			say(cmd.ErrOrStderr(), fmt.Sprintf("No users in %s: the list holds the Grafana users who can see the project", c.projectShown()), "")
			return nil
		},
		Annotations: map[string]string{agent.AnnotationTokenCost: "medium", agent.AnnotationLLMHint: "--project 314 --search <name>", agent.AnnotationRequiredRole: "Viewer"},
	}
	opts.setup(cmd.Flags())
	return cmd
}
