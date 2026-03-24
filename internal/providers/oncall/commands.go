package oncall

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/grafana/grafanactl/internal/format"
	cmdio "github.com/grafana/grafanactl/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ---------------------------------------------------------------------------
// Resource group command builder
// ---------------------------------------------------------------------------

// listOpts configures a list subcommand.
type listOpts struct {
	IO       cmdio.Options
	Resource string // resource name for codec selection (e.g. "integrations")
}

func (o *listOpts) setup(flags *pflag.FlagSet, resource string) {
	o.Resource = resource
	switch resource {
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

// getOpts configures a get subcommand.
type getOpts struct {
	IO cmdio.Options
}

func (o *getOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
}

// newListSubcommand creates a "list" subcommand for a resource group.
// The resource parameter selects the table codec (e.g. "integrations", "alert-groups").
func newListSubcommand[T any](loader OnCallConfigLoader, resource, kind, short string, listFn func(*Client, *cobra.Command) ([]T, error)) *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
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
	opts.setup(cmd.Flags(), resource)
	return cmd
}

// newGetSubcommand creates a "get <id>" subcommand for a resource group.
func newGetSubcommand(loader OnCallConfigLoader, short string, getFn func(*Client, *cobra.Command, string) (any, error)) *cobra.Command {
	opts := &getOpts{}
	cmd := &cobra.Command{
		Use:   "get <id>",
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
// Per-resource group commands: oncall <resource> list|get|...
// ---------------------------------------------------------------------------

func newIntegrationsCmd(loader OnCallConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "integrations",
		Short:   "Manage OnCall integrations.",
		Aliases: []string{"integration"},
	}
	cmd.AddCommand(
		newListSubcommand(loader, "integrations", "Integration", "List OnCall integrations.",
			func(c *Client, cmd *cobra.Command) ([]Integration, error) { return c.ListIntegrations(cmd.Context()) }),
		newGetSubcommand(loader, "Get an integration by ID.", func(c *Client, cmd *cobra.Command, id string) (any, error) {
			return c.GetIntegration(cmd.Context(), id)
		}),
	)
	return cmd
}

func newEscalationChainsCmd(loader OnCallConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "escalation-chains",
		Short:   "Manage escalation chains.",
		Aliases: []string{"escalation-chain", "ec"},
	}
	cmd.AddCommand(
		newListSubcommand(loader, "escalation-chains", "EscalationChain", "List escalation chains.",
			func(c *Client, cmd *cobra.Command) ([]EscalationChain, error) {
				return c.ListEscalationChains(cmd.Context())
			}),
		newGetSubcommand(loader, "Get an escalation chain by ID.", func(c *Client, cmd *cobra.Command, id string) (any, error) {
			return c.GetEscalationChain(cmd.Context(), id)
		}),
	)
	return cmd
}

func newEscalationPoliciesCmd(loader OnCallConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "escalation-policies",
		Short:   "Manage escalation policies.",
		Aliases: []string{"escalation-policy", "ep"},
	}
	cmd.AddCommand(
		newListSubcommand(loader, "escalation-policies", "EscalationPolicy", "List escalation policies.",
			func(c *Client, cmd *cobra.Command) ([]EscalationPolicy, error) {
				return c.ListEscalationPolicies(cmd.Context(), "")
			}),
		newGetSubcommand(loader, "Get an escalation policy by ID.", func(c *Client, cmd *cobra.Command, id string) (any, error) {
			return c.GetEscalationPolicy(cmd.Context(), id)
		}),
	)
	return cmd
}

func newSchedulesCmd(loader OnCallConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "schedules",
		Short:   "Manage OnCall schedules.",
		Aliases: []string{"schedule"},
	}
	cmd.AddCommand(
		newListSubcommand(loader, "schedules", "Schedule", "List OnCall schedules.",
			func(c *Client, cmd *cobra.Command) ([]Schedule, error) { return c.ListSchedules(cmd.Context()) }),
		newGetSubcommand(loader, "Get a schedule by ID.", func(c *Client, cmd *cobra.Command, id string) (any, error) {
			return c.GetSchedule(cmd.Context(), id)
		}),
		newScheduleFinalShiftsCommand(loader),
	)
	return cmd
}

func newShiftsCmd(loader OnCallConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "shifts",
		Short:   "Manage OnCall shifts.",
		Aliases: []string{"shift"},
	}
	cmd.AddCommand(
		newListSubcommand(loader, "shifts", "Shift", "List OnCall shifts.",
			func(c *Client, cmd *cobra.Command) ([]Shift, error) { return c.ListShifts(cmd.Context()) }),
		newGetSubcommand(loader, "Get a shift by ID.", func(c *Client, cmd *cobra.Command, id string) (any, error) {
			return c.GetShift(cmd.Context(), id)
		}),
	)
	return cmd
}

func newRoutesCmd(loader OnCallConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "routes",
		Short:   "Manage OnCall routes.",
		Aliases: []string{"route"},
	}
	cmd.AddCommand(
		newListSubcommand(loader, "routes", "Route", "List OnCall routes.",
			func(c *Client, cmd *cobra.Command) ([]IntegrationRoute, error) {
				return c.ListRoutes(cmd.Context(), "")
			}),
		newGetSubcommand(loader, "Get a route by ID.", func(c *Client, cmd *cobra.Command, id string) (any, error) {
			return c.GetRoute(cmd.Context(), id)
		}),
	)
	return cmd
}

func newWebhooksCmd(loader OnCallConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "webhooks",
		Short:   "Manage outgoing webhooks.",
		Aliases: []string{"webhook"},
	}
	cmd.AddCommand(
		newListSubcommand(loader, "webhooks", "OutgoingWebhook", "List outgoing webhooks.",
			func(c *Client, cmd *cobra.Command) ([]OutgoingWebhook, error) {
				return c.ListOutgoingWebhooks(cmd.Context())
			}),
		newGetSubcommand(loader, "Get an outgoing webhook by ID.", func(c *Client, cmd *cobra.Command, id string) (any, error) {
			return c.GetOutgoingWebhook(cmd.Context(), id)
		}),
	)
	return cmd
}

func newTeamsCmd(loader OnCallConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "teams",
		Short:   "Manage OnCall teams.",
		Aliases: []string{"team"},
	}
	cmd.AddCommand(
		newListSubcommand(loader, "teams", "Team", "List OnCall teams.",
			func(c *Client, cmd *cobra.Command) ([]Team, error) { return c.ListTeams(cmd.Context()) }),
		newGetSubcommand(loader, "Get a team by ID.", func(c *Client, cmd *cobra.Command, id string) (any, error) {
			return c.GetTeam(cmd.Context(), id)
		}),
	)
	return cmd
}

func newUserGroupsCmd(loader OnCallConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "user-groups",
		Short:   "List user groups.",
		Aliases: []string{"user-group"},
	}
	cmd.AddCommand(
		newListSubcommand(loader, "user-groups", "UserGroup", "List user groups.",
			func(c *Client, cmd *cobra.Command) ([]UserGroup, error) { return c.ListUserGroups(cmd.Context()) }),
	)
	return cmd
}

func newSlackChannelsCmd(loader OnCallConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "slack-channels",
		Short:   "List Slack channels.",
		Aliases: []string{"slack-channel"},
	}
	cmd.AddCommand(
		newListSubcommand(loader, "slack-channels", "SlackChannel", "List Slack channels.",
			func(c *Client, cmd *cobra.Command) ([]SlackChannel, error) { return c.ListSlackChannels(cmd.Context()) }),
	)
	return cmd
}

func newAlertsCmd(loader OnCallConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "alerts",
		Short:   "View individual alerts.",
		Aliases: []string{"alert"},
	}
	cmd.AddCommand(
		newListSubcommand(loader, "alerts", "Alert", "List alerts.",
			func(c *Client, cmd *cobra.Command) ([]Alert, error) { return c.ListAlerts(cmd.Context(), "") }),
		newGetSubcommand(loader, "Get an alert by ID.", func(c *Client, cmd *cobra.Command, id string) (any, error) {
			return c.GetAlert(cmd.Context(), id)
		}),
	)
	return cmd
}

func newOrganizationsCmd(loader OnCallConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "organizations",
		Short:   "List organizations.",
		Aliases: []string{"organization", "org"},
	}
	cmd.AddCommand(
		newListSubcommand(loader, "organizations", "Organization", "List organizations.",
			func(c *Client, cmd *cobra.Command) ([]Organization, error) { return c.ListOrganizations(cmd.Context()) }),
		newGetSubcommand(loader, "Get an organization by ID.", func(c *Client, cmd *cobra.Command, id string) (any, error) {
			return c.GetOrganization(cmd.Context(), id)
		}),
	)
	return cmd
}

func newResolutionNotesCmd(loader OnCallConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "resolution-notes",
		Short:   "Manage resolution notes.",
		Aliases: []string{"resolution-note", "rn"},
	}
	cmd.AddCommand(
		newListSubcommand(loader, "resolution-notes", "ResolutionNote", "List resolution notes.",
			func(c *Client, cmd *cobra.Command) ([]ResolutionNote, error) {
				return c.ListResolutionNotes(cmd.Context(), "")
			}),
		newGetSubcommand(loader, "Get a resolution note by ID.", func(c *Client, cmd *cobra.Command, id string) (any, error) {
			return c.GetResolutionNote(cmd.Context(), id)
		}),
	)
	return cmd
}

func newShiftSwapsCmd(loader OnCallConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "shift-swaps",
		Short:   "Manage shift swaps.",
		Aliases: []string{"shift-swap", "ss"},
	}
	cmd.AddCommand(
		newListSubcommand(loader, "shift-swaps", "ShiftSwap", "List shift swaps.",
			func(c *Client, cmd *cobra.Command) ([]ShiftSwap, error) { return c.ListShiftSwaps(cmd.Context()) }),
		newGetSubcommand(loader, "Get a shift swap by ID.", func(c *Client, cmd *cobra.Command, id string) (any, error) {
			return c.GetShiftSwap(cmd.Context(), id)
		}),
	)
	return cmd
}

func newPersonalNotificationRulesCmd(loader OnCallConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "personal-notification-rules",
		Short:   "Manage personal notification rules.",
		Aliases: []string{"personal-notification-rule", "pnr"},
	}
	cmd.AddCommand(
		newListSubcommand(loader, "personal-notification-rules", "PersonalNotificationRule", "List personal notification rules.",
			func(c *Client, cmd *cobra.Command) ([]PersonalNotificationRule, error) {
				return c.ListPersonalNotificationRules(cmd.Context())
			}),
		newGetSubcommand(loader, "Get a personal notification rule by ID.", func(c *Client, cmd *cobra.Command, id string) (any, error) {
			return c.GetPersonalNotificationRule(cmd.Context(), id)
		}),
	)
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
