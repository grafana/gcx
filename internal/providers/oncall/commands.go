package oncall

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/grafana/gcx/internal/deeplink"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/style"
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
	Limit    int64
}

func (o *listOpts) setup(flags *pflag.FlagSet, resource string) {
	o.Resource = resource
	flags.Int64Var(&o.Limit, "limit", 50, "Maximum number of items to return (0 for all)")
	switch resource {
	case "integrations":
		o.IO.RegisterCustomCodec("table", &IntegrationTableCodec{})
		o.IO.RegisterCustomCodec("wide", &IntegrationTableCodec{Wide: true})
	case "escalation-chains":
		o.IO.RegisterCustomCodec("table", &EscalationChainTableCodec{})
	case "escalation-policies":
		o.IO.RegisterCustomCodec("table", &EscalationPolicyTableCodec{})
		o.IO.RegisterCustomCodec("wide", &EscalationPolicyTableCodec{Wide: true})
	case "schedules":
		o.IO.RegisterCustomCodec("table", &ScheduleTableCodec{})
		o.IO.RegisterCustomCodec("wide", &ScheduleTableCodec{Wide: true})
	case "shifts":
		o.IO.RegisterCustomCodec("table", &ShiftTableCodec{})
		o.IO.RegisterCustomCodec("wide", &ShiftTableCodec{Wide: true})
	case "routes":
		o.IO.RegisterCustomCodec("table", &RouteTableCodec{})
		o.IO.RegisterCustomCodec("wide", &RouteTableCodec{Wide: true})
	case "webhooks":
		o.IO.RegisterCustomCodec("table", &WebhookTableCodec{})
		o.IO.RegisterCustomCodec("wide", &WebhookTableCodec{Wide: true})
	case "alert-groups":
		o.IO.RegisterCustomCodec("table", &AlertGroupTableCodec{})
		o.IO.RegisterCustomCodec("wide", &AlertGroupTableCodec{Wide: true})
	case "users":
		o.IO.RegisterCustomCodec("table", &UserTableCodec{})
		o.IO.RegisterCustomCodec("wide", &UserTableCodec{Wide: true})
	case "teams":
		o.IO.RegisterCustomCodec("table", &TeamTableCodec{})
		o.IO.RegisterCustomCodec("wide", &TeamTableCodec{Wide: true})
	case "user-groups":
		o.IO.RegisterCustomCodec("table", &UserGroupTableCodec{})
	case "slack-channels":
		o.IO.RegisterCustomCodec("table", &SlackChannelTableCodec{})
	case "alerts":
		o.IO.RegisterCustomCodec("table", &AlertTableCodec{})
	case "resolution-notes":
		o.IO.RegisterCustomCodec("table", &ResolutionNoteTableCodec{})
		o.IO.RegisterCustomCodec("wide", &ResolutionNoteTableCodec{Wide: true})
	case "organizations":
		o.IO.RegisterCustomCodec("table", &OrganizationTableCodec{})
	case "shift-swaps":
		o.IO.RegisterCustomCodec("table", &ShiftSwapTableCodec{})
		o.IO.RegisterCustomCodec("wide", &ShiftSwapTableCodec{Wide: true})
	case "personal-notification-rules":
		o.IO.RegisterCustomCodec("table", &PersonalNotificationRuleTableCodec{})
		o.IO.RegisterCustomCodec("wide", &PersonalNotificationRuleTableCodec{Wide: true})
	}
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

// getOpts configures a get subcommand.
type getOpts struct {
	IO   cmdio.Options
	Open bool
}

func (o *getOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
	flags.BoolVar(&o.Open, "open", false, "Open the resource in the default browser")
}

// newListSubcommand creates a "list" subcommand using TypedCRUD.
// The resource parameter selects the table codec (e.g. "integrations", "alert-groups").
func newListSubcommand[T adapter.ResourceNamer](
	loader OnCallConfigLoader, resource, kind, short string,
	listFn func(ctx context.Context, client *Client) ([]T, error),
	getFn func(ctx context.Context, client *Client, name string) (*T, error),
	opts ...crudOption[T],
) *cobra.Command {
	listOpts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: short,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := listOpts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			crud, namespace, host, err := NewTypedCRUD(ctx, loader, listFn, getFn, opts...)
			if err != nil {
				return err
			}

			typedObjs, err := crud.List(ctx, listOpts.Limit)
			if err != nil {
				return err
			}

			// Convert TypedObject[T] back to unstructured for output codecs
			objs := make([]unstructured.Unstructured, len(typedObjs))
			for i, typedObj := range typedObjs {
				data, err := json.Marshal(typedObj.Spec)
				if err != nil {
					return fmt.Errorf("failed to marshal %s: %w", kind, err)
				}

				var m map[string]any
				if err := json.Unmarshal(data, &m); err != nil {
					return fmt.Errorf("failed to unmarshal %s: %w", kind, err)
				}

				id := typedObj.Spec.GetResourceName()
				delete(m, "id") // Remove id from spec

				objs[i] = unstructured.Unstructured{Object: map[string]any{
					"apiVersion": APIVersion,
					"kind":       kind,
					"metadata": map[string]any{
						"name":      id,
						"namespace": namespace,
					},
					"spec": m,
				}}
			}

			deeplink.InjectURLs(objs, host)

			return listOpts.IO.Encode(cmd.OutOrStdout(), objs)
		},
	}
	listOpts.setup(cmd.Flags(), resource)
	return cmd
}

// newGetSubcommand creates a "get <id>" subcommand using TypedCRUD.
func newGetSubcommand[T adapter.ResourceNamer](
	loader OnCallConfigLoader, short string,
	getFn func(ctx context.Context, client *Client, name string) (*T, error),
) *cobra.Command {
	getOpts := &getOpts{}
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := getOpts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			crud, _, host, err := NewTypedCRUD(ctx, loader, func(_ context.Context, _ *Client) ([]T, error) { return nil, nil }, getFn)
			if err != nil {
				return err
			}

			typedObj, err := crud.Get(ctx, args[0])
			if err != nil {
				return err
			}

			if getOpts.Open {
				gvk := crud.Descriptor.GroupVersionKind()
				url := deeplink.Resolve(host, gvk, typedObj.Name)
				if url == "" {
					return fmt.Errorf("no deep link URL available for %s/%s", crud.Descriptor.Kind, typedObj.Name)
				}
				cmdio.Info(cmd.ErrOrStderr(), "Opening %s", url)
				return deeplink.Open(url)
			}

			return getOpts.IO.Encode(cmd.OutOrStdout(), typedObj.Spec)
		},
	}
	getOpts.setup(cmd.Flags())
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
			func(ctx context.Context, c *Client) ([]Integration, error) { return c.ListIntegrations(ctx) },
			func(ctx context.Context, c *Client, name string) (*Integration, error) {
				return c.GetIntegration(ctx, name)
			}),
		newGetSubcommand(loader, "Get an integration by ID.",
			func(ctx context.Context, c *Client, name string) (*Integration, error) {
				return c.GetIntegration(ctx, name)
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
			func(ctx context.Context, c *Client) ([]EscalationChain, error) {
				return c.ListEscalationChains(ctx)
			},
			func(ctx context.Context, c *Client, name string) (*EscalationChain, error) {
				return c.GetEscalationChain(ctx, name)
			}),
		newGetSubcommand(loader, "Get an escalation chain by ID.",
			func(ctx context.Context, c *Client, name string) (*EscalationChain, error) {
				return c.GetEscalationChain(ctx, name)
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
			func(ctx context.Context, c *Client) ([]EscalationPolicy, error) {
				return c.ListEscalationPolicies(ctx, "")
			},
			func(ctx context.Context, c *Client, name string) (*EscalationPolicy, error) {
				return c.GetEscalationPolicy(ctx, name)
			}),
		newGetSubcommand(loader, "Get an escalation policy by ID.",
			func(ctx context.Context, c *Client, name string) (*EscalationPolicy, error) {
				return c.GetEscalationPolicy(ctx, name)
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
			func(ctx context.Context, c *Client) ([]Schedule, error) { return c.ListSchedules(ctx) },
			func(ctx context.Context, c *Client, name string) (*Schedule, error) { return c.GetSchedule(ctx, name) }),
		newGetSubcommand(loader, "Get a schedule by ID.",
			func(ctx context.Context, c *Client, name string) (*Schedule, error) { return c.GetSchedule(ctx, name) }),
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
			func(ctx context.Context, c *Client) ([]Shift, error) { return c.ListShifts(ctx) },
			func(ctx context.Context, c *Client, name string) (*Shift, error) { return c.GetShift(ctx, name) }),
		newGetSubcommand(loader, "Get a shift by ID.",
			func(ctx context.Context, c *Client, name string) (*Shift, error) { return c.GetShift(ctx, name) }),
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
			func(ctx context.Context, c *Client) ([]IntegrationRoute, error) {
				return c.ListRoutes(ctx, "")
			},
			func(ctx context.Context, c *Client, name string) (*IntegrationRoute, error) {
				return c.GetRoute(ctx, name)
			}),
		newGetSubcommand(loader, "Get a route by ID.",
			func(ctx context.Context, c *Client, name string) (*IntegrationRoute, error) {
				return c.GetRoute(ctx, name)
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
			func(ctx context.Context, c *Client) ([]OutgoingWebhook, error) {
				return c.ListOutgoingWebhooks(ctx)
			},
			func(ctx context.Context, c *Client, name string) (*OutgoingWebhook, error) {
				return c.GetOutgoingWebhook(ctx, name)
			}),
		newGetSubcommand(loader, "Get an outgoing webhook by ID.",
			func(ctx context.Context, c *Client, name string) (*OutgoingWebhook, error) {
				return c.GetOutgoingWebhook(ctx, name)
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
			func(ctx context.Context, c *Client) ([]Team, error) { return c.ListTeams(ctx) },
			func(ctx context.Context, c *Client, name string) (*Team, error) { return c.GetTeam(ctx, name) }),
		newGetSubcommand(loader, "Get a team by ID.",
			func(ctx context.Context, c *Client, name string) (*Team, error) { return c.GetTeam(ctx, name) }),
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
			func(ctx context.Context, c *Client) ([]UserGroup, error) { return c.ListUserGroups(ctx) },
			nil),
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
			func(ctx context.Context, c *Client) ([]SlackChannel, error) { return c.ListSlackChannels(ctx) },
			nil),
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
		newGetSubcommand(loader, "Get an alert by ID.",
			func(ctx context.Context, c *Client, name string) (*Alert, error) { return c.GetAlert(ctx, name) }),
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
			func(ctx context.Context, c *Client) ([]Organization, error) { return c.ListOrganizations(ctx) },
			func(ctx context.Context, c *Client, name string) (*Organization, error) {
				return c.GetOrganization(ctx, name)
			}),
		newGetSubcommand(loader, "Get an organization by ID.",
			func(ctx context.Context, c *Client, name string) (*Organization, error) {
				return c.GetOrganization(ctx, name)
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
			func(ctx context.Context, c *Client) ([]ResolutionNote, error) {
				return c.ListResolutionNotes(ctx, "")
			},
			func(ctx context.Context, c *Client, name string) (*ResolutionNote, error) {
				return c.GetResolutionNote(ctx, name)
			}),
		newGetSubcommand(loader, "Get a resolution note by ID.",
			func(ctx context.Context, c *Client, name string) (*ResolutionNote, error) {
				return c.GetResolutionNote(ctx, name)
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
			func(ctx context.Context, c *Client) ([]ShiftSwap, error) { return c.ListShiftSwaps(ctx) },
			func(ctx context.Context, c *Client, name string) (*ShiftSwap, error) {
				return c.GetShiftSwap(ctx, name)
			}),
		newGetSubcommand(loader, "Get a shift swap by ID.",
			func(ctx context.Context, c *Client, name string) (*ShiftSwap, error) {
				return c.GetShiftSwap(ctx, name)
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

	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("ID", "NAME", "TYPE", "TEAM", "LINK")
	} else {
		t = style.NewTable("ID", "NAME", "TYPE")
	}

	for _, obj := range items {
		id := obj.GetName()
		name := specStr(obj, "name")
		if !c.Wide {
			name = truncate(name, 50)
		}
		if c.Wide {
			t.Row(id, name, specStr(obj, "type"), orDash(specStr(obj, "team_id")), orDash(specStr(obj, "link")))
		} else {
			t.Row(id, name, specStr(obj, "type"))
		}
	}

	return t.Render(w)
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

	t := style.NewTable("ID", "NAME", "TEAM")
	for _, obj := range items {
		t.Row(obj.GetName(), specStr(obj, "name"), orDash(specStr(obj, "team_id")))
	}
	return t.Render(w)
}

func (c *EscalationChainTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// EscalationPolicyTableCodec renders escalation policies as a table.
type EscalationPolicyTableCodec struct {
	Wide bool
}

func (c *EscalationPolicyTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *EscalationPolicyTableCodec) Encode(w io.Writer, v any) error {
	items, err := toUnstructuredSlice(v)
	if err != nil {
		return err
	}

	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("ID", "CHAIN", "POS", "TYPE", "DURATION", "IMPORTANT", "NOTIFY-SCHEDULE")
	} else {
		t = style.NewTable("ID", "CHAIN", "POS", "TYPE", "DURATION")
	}

	for _, obj := range items {
		id := obj.GetName()
		dur := specInt(obj, "duration")
		durStr := "-"
		if dur > 0 {
			durStr = fmt.Sprintf("%ds", dur)
		}
		if c.Wide {
			important := "false"
			if specBool(obj, "important") {
				important = "true"
			}
			t.Row(id, specStr(obj, "escalation_chain_id"), strconv.Itoa(specInt(obj, "position")), specStr(obj, "type"), durStr, important, orDash(specStr(obj, "notify_on_call_from_schedule")))
		} else {
			t.Row(id, specStr(obj, "escalation_chain_id"), strconv.Itoa(specInt(obj, "position")), specStr(obj, "type"), durStr)
		}
	}

	return t.Render(w)
}

func (c *EscalationPolicyTableCodec) Decode(_ io.Reader, _ any) error {
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

	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("ID", "NAME", "TYPE", "TIMEZONE", "TEAM", "ON-CALL-NOW")
	} else {
		t = style.NewTable("ID", "NAME", "TYPE", "TIMEZONE")
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
			t.Row(id, specStr(obj, "name"), specStr(obj, "type"), tz, orDash(specStr(obj, "team_id")), onCallNow)
		} else {
			t.Row(id, specStr(obj, "name"), specStr(obj, "type"), tz)
		}
	}

	return t.Render(w)
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

	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("ID", "NAME", "URL", "METHOD", "TRIGGER", "ENABLED")
	} else {
		t = style.NewTable("ID", "NAME", "TRIGGER", "ENABLED")
	}

	for _, obj := range items {
		id := obj.GetName()
		enabled := "false"
		if specBool(obj, "is_webhook_enabled") {
			enabled = "true"
		}
		if c.Wide {
			t.Row(id, specStr(obj, "name"), orDash(specStr(obj, "url")), orDash(specStr(obj, "http_method")), specStr(obj, "trigger_type"), enabled)
		} else {
			t.Row(id, specStr(obj, "name"), specStr(obj, "trigger_type"), enabled)
		}
	}

	return t.Render(w)
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

	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("ID", "TITLE", "STATE", "ALERTS", "CREATED", "INTEGRATION")
	} else {
		t = style.NewTable("ID", "TITLE", "STATE", "ALERTS", "CREATED")
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
			t.Row(id, title, specStr(obj, "state"), strconv.Itoa(alerts), created, orDash(specStr(obj, "integration_id")))
		} else {
			t.Row(id, title, specStr(obj, "state"), strconv.Itoa(alerts), created)
		}
	}

	return t.Render(w)
}

func (c *AlertGroupTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// UserTableCodec renders users as a table.
type UserTableCodec struct {
	Wide bool
}

func (c *UserTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *UserTableCodec) Encode(w io.Writer, v any) error {
	items, err := toUnstructuredSlice(v)
	if err != nil {
		return err
	}

	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("ID", "USERNAME", "NAME", "EMAIL", "ROLE", "TIMEZONE")
	} else {
		t = style.NewTable("ID", "USERNAME", "NAME", "ROLE", "TIMEZONE")
	}

	for _, obj := range items {
		if c.Wide {
			t.Row(obj.GetName(), specStr(obj, "username"), orDash(specStr(obj, "name")),
				orDash(specStr(obj, "email")), orDash(specStr(obj, "role")), orDash(specStr(obj, "timezone")))
		} else {
			t.Row(obj.GetName(), specStr(obj, "username"), orDash(specStr(obj, "name")),
				orDash(specStr(obj, "role")), orDash(specStr(obj, "timezone")))
		}
	}

	return t.Render(w)
}

func (c *UserTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// TeamTableCodec renders teams as a table.
type TeamTableCodec struct {
	Wide bool
}

func (c *TeamTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *TeamTableCodec) Encode(w io.Writer, v any) error {
	items, err := toUnstructuredSlice(v)
	if err != nil {
		return err
	}

	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("ID", "NAME", "EMAIL", "GRAFANA-ID")
	} else {
		t = style.NewTable("ID", "NAME", "EMAIL")
	}

	for _, obj := range items {
		if c.Wide {
			t.Row(obj.GetName(), specStr(obj, "name"), orDash(specStr(obj, "email")), strconv.Itoa(specInt(obj, "grafana_id")))
		} else {
			t.Row(obj.GetName(), specStr(obj, "name"), orDash(specStr(obj, "email")))
		}
	}

	return t.Render(w)
}

func (c *TeamTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// ShiftTableCodec renders shifts as a table.
type ShiftTableCodec struct {
	Wide bool
}

func (c *ShiftTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *ShiftTableCodec) Encode(w io.Writer, v any) error {
	items, err := toUnstructuredSlice(v)
	if err != nil {
		return err
	}

	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("ID", "NAME", "TYPE", "START", "DURATION", "FREQUENCY", "INTERVAL", "TEAM")
	} else {
		t = style.NewTable("ID", "NAME", "TYPE", "START", "DURATION")
	}

	for _, obj := range items {
		id := obj.GetName()
		dur := specInt(obj, "duration")
		durStr := "-"
		if dur > 0 {
			durStr = fmt.Sprintf("%ds", dur)
		}
		start := orDash(specStr(obj, "start"))
		if c.Wide {
			freq := orDash(specStr(obj, "frequency"))
			interval := specInt(obj, "interval")
			intervalStr := "-"
			if interval > 0 {
				intervalStr = strconv.Itoa(interval)
			}
			t.Row(id, specStr(obj, "name"), specStr(obj, "type"), start, durStr, freq, intervalStr, orDash(specStr(obj, "team_id")))
		} else {
			t.Row(id, specStr(obj, "name"), specStr(obj, "type"), start, durStr)
		}
	}

	return t.Render(w)
}

func (c *ShiftTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// RouteTableCodec renders routes as a table.
type RouteTableCodec struct {
	Wide bool
}

func (c *RouteTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *RouteTableCodec) Encode(w io.Writer, v any) error {
	items, err := toUnstructuredSlice(v)
	if err != nil {
		return err
	}

	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("ID", "INTEGRATION", "CHAIN", "POS", "ROUTING-TYPE", "REGEX", "LAST")
	} else {
		t = style.NewTable("ID", "INTEGRATION", "CHAIN", "POS", "ROUTING-TYPE")
	}

	for _, obj := range items {
		id := obj.GetName()
		if c.Wide {
			isLast := "false"
			if specBool(obj, "is_the_last_route") {
				isLast = "true"
			}
			regex := orDash(specStr(obj, "routing_regex"))
			if len(regex) > 40 {
				regex = regex[:37] + "..."
			}
			t.Row(id, specStr(obj, "integration_id"), orDash(specStr(obj, "escalation_chain_id")), strconv.Itoa(specInt(obj, "position")), orDash(specStr(obj, "routing_type")), regex, isLast)
		} else {
			t.Row(id, specStr(obj, "integration_id"), orDash(specStr(obj, "escalation_chain_id")), strconv.Itoa(specInt(obj, "position")), orDash(specStr(obj, "routing_type")))
		}
	}

	return t.Render(w)
}

func (c *RouteTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// UserGroupTableCodec renders user groups as a table.
type UserGroupTableCodec struct{}

func (c *UserGroupTableCodec) Format() format.Format { return "table" }

func (c *UserGroupTableCodec) Encode(w io.Writer, v any) error {
	items, err := toUnstructuredSlice(v)
	if err != nil {
		return err
	}

	t := style.NewTable("ID", "NAME", "TYPE", "HANDLE")
	for _, obj := range items {
		t.Row(obj.GetName(), orDash(specStr(obj, "name")), orDash(specStr(obj, "type")), orDash(specStr(obj, "handle")))
	}
	return t.Render(w)
}

func (c *UserGroupTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// SlackChannelTableCodec renders Slack channels as a table.
type SlackChannelTableCodec struct{}

func (c *SlackChannelTableCodec) Format() format.Format { return "table" }

func (c *SlackChannelTableCodec) Encode(w io.Writer, v any) error {
	items, err := toUnstructuredSlice(v)
	if err != nil {
		return err
	}

	t := style.NewTable("ID", "NAME", "SLACK-ID")
	for _, obj := range items {
		t.Row(obj.GetName(), orDash(specStr(obj, "name")), orDash(specStr(obj, "slack_id")))
	}
	return t.Render(w)
}

func (c *SlackChannelTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// AlertTableCodec renders alerts as a table.
type AlertTableCodec struct{}

func (c *AlertTableCodec) Format() format.Format { return "table" }

func (c *AlertTableCodec) Encode(w io.Writer, v any) error {
	items, err := toUnstructuredSlice(v)
	if err != nil {
		return err
	}

	t := style.NewTable("ID", "ALERT-GROUP", "CREATED")
	for _, obj := range items {
		created := specStr(obj, "created_at")
		if len(created) > 16 {
			created = created[:16]
		}
		t.Row(obj.GetName(), specStr(obj, "alert_group_id"), orDash(created))
	}
	return t.Render(w)
}

func (c *AlertTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// ResolutionNoteTableCodec renders resolution notes as a table.
type ResolutionNoteTableCodec struct {
	Wide bool
}

func (c *ResolutionNoteTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *ResolutionNoteTableCodec) Encode(w io.Writer, v any) error {
	items, err := toUnstructuredSlice(v)
	if err != nil {
		return err
	}

	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("ID", "ALERT-GROUP", "SOURCE", "CREATED", "TEXT")
	} else {
		t = style.NewTable("ID", "ALERT-GROUP", "SOURCE", "CREATED")
	}

	for _, obj := range items {
		created := specStr(obj, "created_at")
		if len(created) > 16 {
			created = created[:16]
		}
		if c.Wide {
			text := specStr(obj, "text")
			if len(text) > 60 {
				text = text[:57] + "..."
			}
			t.Row(obj.GetName(), specStr(obj, "alert_group_id"), orDash(specStr(obj, "source")), orDash(created), orDash(text))
		} else {
			t.Row(obj.GetName(), specStr(obj, "alert_group_id"), orDash(specStr(obj, "source")), orDash(created))
		}
	}

	return t.Render(w)
}

func (c *ResolutionNoteTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// OrganizationTableCodec renders organizations as a table.
type OrganizationTableCodec struct{}

func (c *OrganizationTableCodec) Format() format.Format { return "table" }

func (c *OrganizationTableCodec) Encode(w io.Writer, v any) error {
	items, err := toUnstructuredSlice(v)
	if err != nil {
		return err
	}

	t := style.NewTable("ID", "NAME", "SLUG")
	for _, obj := range items {
		t.Row(obj.GetName(), orDash(specStr(obj, "name")), orDash(specStr(obj, "slug")))
	}
	return t.Render(w)
}

func (c *OrganizationTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// ShiftSwapTableCodec renders shift swaps as a table.
type ShiftSwapTableCodec struct {
	Wide bool
}

func (c *ShiftSwapTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *ShiftSwapTableCodec) Encode(w io.Writer, v any) error {
	items, err := toUnstructuredSlice(v)
	if err != nil {
		return err
	}

	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("ID", "SCHEDULE", "STATUS", "START", "END", "BENEFICIARY", "BENEFACTOR", "CREATED")
	} else {
		t = style.NewTable("ID", "SCHEDULE", "STATUS", "START", "END")
	}

	for _, obj := range items {
		id := obj.GetName()
		start := specStr(obj, "swap_start")
		if len(start) > 16 {
			start = start[:16]
		}
		end := specStr(obj, "swap_end")
		if len(end) > 16 {
			end = end[:16]
		}
		if c.Wide {
			created := specStr(obj, "created_at")
			if len(created) > 16 {
				created = created[:16]
			}
			t.Row(id, orDash(specStr(obj, "schedule")), orDash(specStr(obj, "status")), orDash(start), orDash(end), orDash(specStr(obj, "beneficiary")), orDash(specStr(obj, "benefactor")), orDash(created))
		} else {
			t.Row(id, orDash(specStr(obj, "schedule")), orDash(specStr(obj, "status")), orDash(start), orDash(end))
		}
	}

	return t.Render(w)
}

func (c *ShiftSwapTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// PersonalNotificationRuleTableCodec renders personal notification rules as a table.
type PersonalNotificationRuleTableCodec struct {
	Wide bool
}

func (c *PersonalNotificationRuleTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *PersonalNotificationRuleTableCodec) Encode(w io.Writer, v any) error {
	items, err := toUnstructuredSlice(v)
	if err != nil {
		return err
	}

	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("ID", "USER", "STEP", "TYPE", "DURATION", "CHANNEL")
	} else {
		t = style.NewTable("ID", "USER", "STEP", "TYPE", "DURATION")
	}

	for _, obj := range items {
		id := obj.GetName()
		dur := specInt(obj, "duration")
		durStr := "-"
		if dur > 0 {
			durStr = fmt.Sprintf("%ds", dur)
		}
		if c.Wide {
			t.Row(id, orDash(specStr(obj, "user_id")), strconv.Itoa(specInt(obj, "step")), orDash(specStr(obj, "type")), durStr, orDash(specStr(obj, "notification_channel_id")))
		} else {
			t.Row(id, orDash(specStr(obj, "user_id")), strconv.Itoa(specInt(obj, "step")), orDash(specStr(obj, "type")), durStr)
		}
	}

	return t.Render(w)
}

func (c *PersonalNotificationRuleTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}
