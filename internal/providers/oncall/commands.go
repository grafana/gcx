package oncall

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"text/tabwriter"

	cmdio "github.com/grafana/grafanactl/cmd/grafanactl/io"
	"github.com/grafana/grafanactl/internal/format"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ---------------------------------------------------------------------------
// list command
// ---------------------------------------------------------------------------

type listOpts struct {
	IO   cmdio.Options
	Kind string
}

func (o *listOpts) setup(flags *pflag.FlagSet, kind string) {
	o.Kind = kind
	switch kind {
	case "integrations":
		o.IO.RegisterCustomCodec("table", &IntegrationTableCodec{})
		o.IO.RegisterCustomCodec("wide", &IntegrationTableCodec{Wide: true})
	case "escalation-chains":
		o.IO.RegisterCustomCodec("table", &EscalationChainTableCodec{})
	case "schedules":
		o.IO.RegisterCustomCodec("table", &ScheduleTableCodec{})
		o.IO.RegisterCustomCodec("wide", &ScheduleTableCodec{Wide: true})
	case "webhooks":
		o.IO.RegisterCustomCodec("table", &WebhookTableCodec{})
		o.IO.RegisterCustomCodec("wide", &WebhookTableCodec{Wide: true})
	case "alert-groups":
		o.IO.RegisterCustomCodec("table", &AlertGroupTableCodec{})
		o.IO.RegisterCustomCodec("wide", &AlertGroupTableCodec{Wide: true})
	case "users":
		o.IO.RegisterCustomCodec("table", &UserTableCodec{})
	case "teams":
		o.IO.RegisterCustomCodec("table", &TeamTableCodec{})
	}
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newListCommand(loader OnCallConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list <resource-type>",
		Short: "List OnCall resources.",
	}

	cmd.AddCommand(
		newTypedListSubcommand(loader, "integrations", "List OnCall integrations.", "Integration",
			func(c *Client, cmd *cobra.Command) ([]Integration, error) { return c.ListIntegrations(cmd.Context()) }),
		newTypedListSubcommand(loader, "escalation-chains", "List escalation chains.", "EscalationChain",
			func(c *Client, cmd *cobra.Command) ([]EscalationChain, error) {
				return c.ListEscalationChains(cmd.Context())
			}),
		newTypedListSubcommand(loader, "schedules", "List OnCall schedules.", "Schedule",
			func(c *Client, cmd *cobra.Command) ([]Schedule, error) { return c.ListSchedules(cmd.Context()) }),
		newTypedListSubcommand(loader, "shifts", "List OnCall shifts.", "Shift",
			func(c *Client, cmd *cobra.Command) ([]Shift, error) { return c.ListShifts(cmd.Context()) }),
		newTypedListSubcommand(loader, "routes", "List OnCall routes.", "Route",
			func(c *Client, cmd *cobra.Command) ([]IntegrationRoute, error) {
				return c.ListRoutes(cmd.Context(), "")
			}),
		newTypedListSubcommand(loader, "webhooks", "List outgoing webhooks.", "OutgoingWebhook",
			func(c *Client, cmd *cobra.Command) ([]OutgoingWebhook, error) {
				return c.ListOutgoingWebhooks(cmd.Context())
			}),
		newTypedListSubcommand(loader, "alert-groups", "List alert groups.", "AlertGroup",
			func(c *Client, cmd *cobra.Command) ([]AlertGroup, error) { return c.ListAlertGroups(cmd.Context()) }),
		newTypedListSubcommand(loader, "users", "List OnCall users.", "User",
			func(c *Client, cmd *cobra.Command) ([]User, error) { return c.ListUsers(cmd.Context()) }),
		newTypedListSubcommand(loader, "teams", "List OnCall teams.", "Team",
			func(c *Client, cmd *cobra.Command) ([]Team, error) { return c.ListTeams(cmd.Context()) }),
		newTypedListSubcommand(loader, "escalation-policies", "List escalation policies.", "EscalationPolicy",
			func(c *Client, cmd *cobra.Command) ([]EscalationPolicy, error) {
				return c.ListEscalationPolicies(cmd.Context(), "")
			}),
		newTypedListSubcommand(loader, "user-groups", "List user groups.", "UserGroup",
			func(c *Client, cmd *cobra.Command) ([]UserGroup, error) { return c.ListUserGroups(cmd.Context()) }),
		newTypedListSubcommand(loader, "slack-channels", "List Slack channels.", "SlackChannel",
			func(c *Client, cmd *cobra.Command) ([]SlackChannel, error) { return c.ListSlackChannels(cmd.Context()) }),
	)

	return cmd
}

// newTypedListSubcommand creates a list subcommand that always converts typed items
// to unstructured K8s envelope objects. All output formats (table, wide, json, yaml)
// receive the same data shape, complying with Pattern 13 (format-agnostic data fetching).
func newTypedListSubcommand[T any](loader OnCallConfigLoader, name, short, kind string, listFn func(*Client, *cobra.Command) ([]T, error)) *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   name,
		Short: short,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			client, namespace, err := loader.LoadOnCallClient(cmd.Context())
			if err != nil {
				return err
			}

			items, err := listFn(client, cmd)
			if err != nil {
				return err
			}

			objs, err := itemsToUnstructured(items, kind, "id", namespace)
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), objs)
		},
	}
	opts.setup(cmd.Flags(), name)
	return cmd
}

// itemsToUnstructured converts a typed slice to []unstructured.Unstructured with
// proper ID extraction. The idField value is moved to metadata.name and removed
// from the spec, matching the K8s envelope convention.
func itemsToUnstructured[T any](items []T, kind, idField, namespace string) ([]unstructured.Unstructured, error) {
	var objs []unstructured.Unstructured
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

// ---------------------------------------------------------------------------
// get command
// ---------------------------------------------------------------------------

type getOpts struct {
	IO cmdio.Options
}

func (o *getOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
}

func newGetCommand(loader OnCallConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <resource-type> <id>",
		Short: "Get a single OnCall resource by ID.",
	}

	cmd.AddCommand(
		newGetSubcommand(loader, "integration", "Get an integration by ID.", func(c *Client, cmd *cobra.Command, id string) (any, error) {
			return c.GetIntegration(cmd.Context(), id)
		}),
		newGetSubcommand(loader, "escalation-chain", "Get an escalation chain by ID.", func(c *Client, cmd *cobra.Command, id string) (any, error) {
			return c.GetEscalationChain(cmd.Context(), id)
		}),
		newGetSubcommand(loader, "schedule", "Get a schedule by ID.", func(c *Client, cmd *cobra.Command, id string) (any, error) {
			return c.GetSchedule(cmd.Context(), id)
		}),
		newGetSubcommand(loader, "shift", "Get a shift by ID.", func(c *Client, cmd *cobra.Command, id string) (any, error) {
			return c.GetShift(cmd.Context(), id)
		}),
		newGetSubcommand(loader, "route", "Get a route by ID.", func(c *Client, cmd *cobra.Command, id string) (any, error) {
			return c.GetRoute(cmd.Context(), id)
		}),
		newGetSubcommand(loader, "webhook", "Get an outgoing webhook by ID.", func(c *Client, cmd *cobra.Command, id string) (any, error) {
			return c.GetOutgoingWebhook(cmd.Context(), id)
		}),
		newGetSubcommand(loader, "alert-group", "Get an alert group by ID.", func(c *Client, cmd *cobra.Command, id string) (any, error) {
			return c.GetAlertGroup(cmd.Context(), id)
		}),
		newGetSubcommand(loader, "user", "Get a user by ID.", func(c *Client, cmd *cobra.Command, id string) (any, error) {
			return c.GetUser(cmd.Context(), id)
		}),
		newGetSubcommand(loader, "team", "Get a team by ID.", func(c *Client, cmd *cobra.Command, id string) (any, error) {
			return c.GetTeam(cmd.Context(), id)
		}),
		newGetSubcommand(loader, "escalation-policy", "Get an escalation policy by ID.", func(c *Client, cmd *cobra.Command, id string) (any, error) {
			return c.GetEscalationPolicy(cmd.Context(), id)
		}),
	)

	return cmd
}

func newGetSubcommand(loader OnCallConfigLoader, name, short string, getFn func(*Client, *cobra.Command, string) (any, error)) *cobra.Command {
	opts := &getOpts{}
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

			item, err := getFn(client, cmd, args[0])
			if err != nil {
				return err
			}

			return opts.IO.Encode(cmd.OutOrStdout(), item)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// Table codecs — all accept []unstructured.Unstructured (Pattern 13 compliant)
// ---------------------------------------------------------------------------

// specStr extracts a string field from an unstructured object's spec.
func specStr(obj unstructured.Unstructured, key string) string {
	spec, ok := obj.Object["spec"].(map[string]any)
	if !ok {
		return ""
	}
	v, ok := spec[key]
	if !ok {
		return ""
	}
	return fmt.Sprint(v)
}

// specInt extracts an int field from an unstructured object's spec.
func specInt(obj unstructured.Unstructured, key string) int {
	spec, ok := obj.Object["spec"].(map[string]any)
	if !ok {
		return 0
	}
	v, ok := spec[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return 0
	}
}

// specBool extracts a bool field from an unstructured object's spec.
func specBool(obj unstructured.Unstructured, key string) bool {
	spec, ok := obj.Object["spec"].(map[string]any)
	if !ok {
		return false
	}
	v, _ := spec[key].(bool)
	return v
}

func toUnstructuredSlice(v any) ([]unstructured.Unstructured, error) {
	items, ok := v.([]unstructured.Unstructured)
	if !ok {
		return nil, errors.New("invalid data type for table codec: expected []unstructured.Unstructured")
	}
	return items, nil
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func truncate(s string, maxLen int) string {
	if len(s) > maxLen {
		return s[:maxLen-3] + "..."
	}
	return s
}

// IntegrationTableCodec renders integrations as a table.
type IntegrationTableCodec struct {
	Wide bool
}

func (c *IntegrationTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *IntegrationTableCodec) Encode(w io.Writer, v any) error {
	items, err := toUnstructuredSlice(v)
	if err != nil {
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if c.Wide {
		fmt.Fprintln(tw, "ID\tNAME\tTYPE\tTEAM\tLINK")
	} else {
		fmt.Fprintln(tw, "ID\tNAME\tTYPE")
	}

	for _, obj := range items {
		id := obj.GetName()
		name := specStr(obj, "name")
		if !c.Wide {
			name = truncate(name, 50)
		}
		if c.Wide {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", id, name, specStr(obj, "type"), orDash(specStr(obj, "team_id")), orDash(specStr(obj, "link")))
		} else {
			fmt.Fprintf(tw, "%s\t%s\t%s\n", id, name, specStr(obj, "type"))
		}
	}

	return tw.Flush()
}

func (c *IntegrationTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// EscalationChainTableCodec renders escalation chains as a table.
type EscalationChainTableCodec struct{}

func (c *EscalationChainTableCodec) Format() format.Format { return "table" }

func (c *EscalationChainTableCodec) Encode(w io.Writer, v any) error {
	items, err := toUnstructuredSlice(v)
	if err != nil {
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tTEAM")

	for _, obj := range items {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", obj.GetName(), specStr(obj, "name"), orDash(specStr(obj, "team_id")))
	}

	return tw.Flush()
}

func (c *EscalationChainTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// ScheduleTableCodec renders schedules as a table.
type ScheduleTableCodec struct {
	Wide bool
}

func (c *ScheduleTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *ScheduleTableCodec) Encode(w io.Writer, v any) error {
	items, err := toUnstructuredSlice(v)
	if err != nil {
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if c.Wide {
		fmt.Fprintln(tw, "ID\tNAME\tTYPE\tTIMEZONE\tTEAM\tON-CALL-NOW")
	} else {
		fmt.Fprintln(tw, "ID\tNAME\tTYPE\tTIMEZONE")
	}

	for _, obj := range items {
		id := obj.GetName()
		tz := orDash(specStr(obj, "time_zone"))
		if c.Wide {
			onCallNow := "-"
			if spec, ok := obj.Object["spec"].(map[string]any); ok {
				if arr, ok := spec["on_call_now"].([]any); ok && len(arr) > 0 {
					onCallNow = fmt.Sprintf("%d users", len(arr))
				}
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", id, specStr(obj, "name"), specStr(obj, "type"), tz, orDash(specStr(obj, "team_id")), onCallNow)
		} else {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", id, specStr(obj, "name"), specStr(obj, "type"), tz)
		}
	}

	return tw.Flush()
}

func (c *ScheduleTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// WebhookTableCodec renders outgoing webhooks as a table.
type WebhookTableCodec struct {
	Wide bool
}

func (c *WebhookTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *WebhookTableCodec) Encode(w io.Writer, v any) error {
	items, err := toUnstructuredSlice(v)
	if err != nil {
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if c.Wide {
		fmt.Fprintln(tw, "ID\tNAME\tURL\tMETHOD\tTRIGGER\tENABLED")
	} else {
		fmt.Fprintln(tw, "ID\tNAME\tTRIGGER\tENABLED")
	}

	for _, obj := range items {
		id := obj.GetName()
		enabled := "false"
		if specBool(obj, "is_webhook_enabled") {
			enabled = "true"
		}
		if c.Wide {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", id, specStr(obj, "name"), orDash(specStr(obj, "url")), orDash(specStr(obj, "http_method")), specStr(obj, "trigger_type"), enabled)
		} else {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", id, specStr(obj, "name"), specStr(obj, "trigger_type"), enabled)
		}
	}

	return tw.Flush()
}

func (c *WebhookTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// AlertGroupTableCodec renders alert groups as a table.
type AlertGroupTableCodec struct {
	Wide bool
}

func (c *AlertGroupTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *AlertGroupTableCodec) Encode(w io.Writer, v any) error {
	items, err := toUnstructuredSlice(v)
	if err != nil {
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if c.Wide {
		fmt.Fprintln(tw, "ID\tTITLE\tSTATE\tALERTS\tCREATED\tINTEGRATION")
	} else {
		fmt.Fprintln(tw, "ID\tTITLE\tSTATE\tALERTS\tCREATED")
	}

	for _, obj := range items {
		id := obj.GetName()
		title := specStr(obj, "title")
		if title == "" {
			title = specStr(obj, "web_title")
		}
		if !c.Wide {
			title = truncate(title, 50)
		}
		created := specStr(obj, "created_at")
		if len(created) > 16 {
			created = created[:16]
		}
		created = orDash(created)
		alerts := specInt(obj, "alerts_count")
		if c.Wide {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\t%s\n", id, title, specStr(obj, "state"), alerts, created, orDash(specStr(obj, "integration_id")))
		} else {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\n", id, title, specStr(obj, "state"), alerts, created)
		}
	}

	return tw.Flush()
}

func (c *AlertGroupTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// UserTableCodec renders users as a table.
type UserTableCodec struct{}

func (c *UserTableCodec) Format() format.Format { return "table" }

func (c *UserTableCodec) Encode(w io.Writer, v any) error {
	items, err := toUnstructuredSlice(v)
	if err != nil {
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tUSERNAME\tNAME\tROLE\tTIMEZONE")

	for _, obj := range items {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			obj.GetName(), specStr(obj, "username"), orDash(specStr(obj, "name")),
			orDash(specStr(obj, "role")), orDash(specStr(obj, "timezone")))
	}

	return tw.Flush()
}

func (c *UserTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// TeamTableCodec renders teams as a table.
type TeamTableCodec struct{}

func (c *TeamTableCodec) Format() format.Format { return "table" }

func (c *TeamTableCodec) Encode(w io.Writer, v any) error {
	items, err := toUnstructuredSlice(v)
	if err != nil {
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tEMAIL")

	for _, obj := range items {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", obj.GetName(), specStr(obj, "name"), orDash(specStr(obj, "email")))
	}

	return tw.Flush()
}

func (c *TeamTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}
