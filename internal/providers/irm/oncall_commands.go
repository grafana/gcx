package irm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"

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

type listOpts struct {
	IO       cmdio.Options
	Resource string
}

func (o *listOpts) setup(flags *pflag.FlagSet, resource string) {
	o.Resource = resource
	switch resource {
	case "integrations":
		o.IO.RegisterCustomCodec("table", &integrationTableCodec{})
		o.IO.RegisterCustomCodec("wide", &integrationTableCodec{Wide: true})
	case "escalation-chains":
		o.IO.RegisterCustomCodec("table", &escalationChainTableCodec{})
	case "escalation-policies":
		o.IO.RegisterCustomCodec("table", &escalationPolicyTableCodec{})
		o.IO.RegisterCustomCodec("wide", &escalationPolicyTableCodec{Wide: true})
	case "schedules":
		o.IO.RegisterCustomCodec("table", &scheduleTableCodec{})
		o.IO.RegisterCustomCodec("wide", &scheduleTableCodec{Wide: true})
	case "shifts":
		o.IO.RegisterCustomCodec("table", &shiftTableCodec{})
		o.IO.RegisterCustomCodec("wide", &shiftTableCodec{Wide: true})
	case "routes":
		o.IO.RegisterCustomCodec("table", &routeTableCodec{})
		o.IO.RegisterCustomCodec("wide", &routeTableCodec{Wide: true})
	case "webhooks":
		o.IO.RegisterCustomCodec("table", &webhookTableCodec{})
		o.IO.RegisterCustomCodec("wide", &webhookTableCodec{Wide: true})
	case "alert-groups":
		o.IO.RegisterCustomCodec("table", &alertGroupTableCodec{})
		o.IO.RegisterCustomCodec("wide", &alertGroupTableCodec{Wide: true})
	case "users":
		o.IO.RegisterCustomCodec("table", &userTableCodec{})
		o.IO.RegisterCustomCodec("wide", &userTableCodec{Wide: true})
	case "teams":
		o.IO.RegisterCustomCodec("table", &teamTableCodec{})
	case "user-groups":
		o.IO.RegisterCustomCodec("table", &userGroupTableCodec{})
	case "slack-channels":
		o.IO.RegisterCustomCodec("table", &slackChannelTableCodec{})
	case "alerts":
		o.IO.RegisterCustomCodec("table", &alertTableCodec{})
	case "organizations":
		o.IO.RegisterCustomCodec("table", &organizationTableCodec{})
	case "resolution-notes":
		o.IO.RegisterCustomCodec("table", &resolutionNoteTableCodec{})
		o.IO.RegisterCustomCodec("wide", &resolutionNoteTableCodec{Wide: true})
	case "shift-swaps":
		o.IO.RegisterCustomCodec("table", &shiftSwapTableCodec{})
		o.IO.RegisterCustomCodec("wide", &shiftSwapTableCodec{Wide: true})
	}
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

type getOpts struct {
	IO cmdio.Options
}

func (o *getOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
}

// newListSubcommand creates a "list" subcommand using TypedCRUD.
func newListSubcommand[T adapter.ResourceNamer](
	loader OnCallConfigLoader, resource, kind, short string, idField string,
	listFn func(ctx context.Context, client OnCallAPI) ([]T, error),
	getFn func(ctx context.Context, client OnCallAPI, name string) (*T, error),
	opts ...crudOption[T],
) *cobra.Command {
	lo := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: short,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := lo.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			crud, namespace, err := newTypedCRUD(ctx, loader, listFn, getFn, opts...)
			if err != nil {
				return err
			}

			typedObjs, err := crud.List(ctx, 0)
			if err != nil {
				return err
			}

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
				delete(m, idField)

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

			return lo.IO.Encode(cmd.OutOrStdout(), objs)
		},
	}
	lo.setup(cmd.Flags(), resource)
	return cmd
}

// newGetSubcommand creates a "get <id>" subcommand using TypedCRUD.
func newGetSubcommand[T adapter.ResourceNamer](
	loader OnCallConfigLoader, short string,
	getFn func(ctx context.Context, client OnCallAPI, name string) (*T, error),
) *cobra.Command {
	go2 := &getOpts{}
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := go2.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			crud, _, err := newTypedCRUD(ctx, loader, func(_ context.Context, _ OnCallAPI) ([]T, error) { return nil, nil }, getFn)
			if err != nil {
				return err
			}

			typedObj, err := crud.Get(ctx, args[0])
			if err != nil {
				return err
			}

			return go2.IO.Encode(cmd.OutOrStdout(), typedObj.Spec)
		},
	}
	go2.setup(cmd.Flags())
	return cmd
}

// crudOption configures optional CRUD operations on a TypedCRUD instance.
type crudOption[T adapter.ResourceNamer] func(client OnCallAPI, crud *adapter.TypedCRUD[T])

func newTypedCRUD[T adapter.ResourceNamer](
	ctx context.Context,
	loader OnCallConfigLoader,
	listFn func(ctx context.Context, client OnCallAPI) ([]T, error),
	getFn func(ctx context.Context, client OnCallAPI, name string) (*T, error),
	opts ...crudOption[T],
) (*adapter.TypedCRUD[T], string, error) {
	client, namespace, err := loader.LoadOnCallClient(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("failed to load IRM OnCall config: %w", err)
	}

	crud := &adapter.TypedCRUD[T]{
		ListFn:      adapter.LimitedListFn(func(ctx context.Context) ([]T, error) { return listFn(ctx, client) }),
		StripFields: defaultStripFields,
		Namespace:   namespace,
	}

	if getFn != nil {
		crud.GetFn = func(ctx context.Context, name string) (*T, error) { return getFn(ctx, client, name) }
	} else {
		crud.GetFn = func(_ context.Context, _ string) (*T, error) { return nil, errors.ErrUnsupported }
	}

	for _, opt := range opts {
		opt(client, crud)
	}

	return crud, namespace, nil
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
		newListSubcommand(loader, "integrations", "Integration", "List OnCall integrations.", "id",
			func(ctx context.Context, c OnCallAPI) ([]Integration, error) { return c.ListIntegrations(ctx) },
			func(ctx context.Context, c OnCallAPI, name string) (*Integration, error) {
				return c.GetIntegration(ctx, name)
			}),
		newGetSubcommand(loader, "Get an integration by ID.",
			func(ctx context.Context, c OnCallAPI, name string) (*Integration, error) {
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
		newListSubcommand(loader, "escalation-chains", "EscalationChain", "List escalation chains.", "id",
			func(ctx context.Context, c OnCallAPI) ([]EscalationChain, error) {
				return c.ListEscalationChains(ctx)
			},
			func(ctx context.Context, c OnCallAPI, name string) (*EscalationChain, error) {
				return c.GetEscalationChain(ctx, name)
			}),
		newGetSubcommand(loader, "Get an escalation chain by ID.",
			func(ctx context.Context, c OnCallAPI, name string) (*EscalationChain, error) {
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
		newListSubcommand(loader, "escalation-policies", "EscalationPolicy", "List escalation policies.", "id",
			func(ctx context.Context, c OnCallAPI) ([]EscalationPolicy, error) {
				return c.ListEscalationPolicies(ctx, "")
			},
			func(ctx context.Context, c OnCallAPI, name string) (*EscalationPolicy, error) {
				return c.GetEscalationPolicy(ctx, name)
			}),
		newGetSubcommand(loader, "Get an escalation policy by ID.",
			func(ctx context.Context, c OnCallAPI, name string) (*EscalationPolicy, error) {
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
		newListSubcommand(loader, "schedules", "Schedule", "List OnCall schedules.", "id",
			func(ctx context.Context, c OnCallAPI) ([]Schedule, error) { return c.ListSchedules(ctx) },
			func(ctx context.Context, c OnCallAPI, name string) (*Schedule, error) {
				return c.GetSchedule(ctx, name)
			}),
		newGetSubcommand(loader, "Get a schedule by ID.",
			func(ctx context.Context, c OnCallAPI, name string) (*Schedule, error) {
				return c.GetSchedule(ctx, name)
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
		newListSubcommand(loader, "shifts", "Shift", "List OnCall shifts.", "id",
			func(ctx context.Context, c OnCallAPI) ([]Shift, error) { return c.ListShifts(ctx) },
			func(ctx context.Context, c OnCallAPI, name string) (*Shift, error) { return c.GetShift(ctx, name) }),
		newGetSubcommand(loader, "Get a shift by ID.",
			func(ctx context.Context, c OnCallAPI, name string) (*Shift, error) { return c.GetShift(ctx, name) }),
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
		newListSubcommand(loader, "routes", "Route", "List OnCall routes.", "id",
			func(ctx context.Context, c OnCallAPI) ([]Route, error) { return c.ListRoutes(ctx, "") },
			func(ctx context.Context, c OnCallAPI, name string) (*Route, error) { return c.GetRoute(ctx, name) }),
		newGetSubcommand(loader, "Get a route by ID.",
			func(ctx context.Context, c OnCallAPI, name string) (*Route, error) { return c.GetRoute(ctx, name) }),
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
		newListSubcommand(loader, "webhooks", "Webhook", "List outgoing webhooks.", "id",
			func(ctx context.Context, c OnCallAPI) ([]Webhook, error) { return c.ListWebhooks(ctx) },
			func(ctx context.Context, c OnCallAPI, name string) (*Webhook, error) {
				return c.GetWebhook(ctx, name)
			}),
		newGetSubcommand(loader, "Get an outgoing webhook by ID.",
			func(ctx context.Context, c OnCallAPI, name string) (*Webhook, error) {
				return c.GetWebhook(ctx, name)
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
		newListSubcommand(loader, "teams", "Team", "List OnCall teams.", "id",
			func(ctx context.Context, c OnCallAPI) ([]Team, error) { return c.ListTeams(ctx) },
			func(ctx context.Context, c OnCallAPI, name string) (*Team, error) { return c.GetTeam(ctx, name) }),
		newGetSubcommand(loader, "Get a team by ID.",
			func(ctx context.Context, c OnCallAPI, name string) (*Team, error) { return c.GetTeam(ctx, name) }),
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
		newListSubcommand[UserGroup](loader, "user-groups", "UserGroup", "List user groups.", "id",
			func(ctx context.Context, c OnCallAPI) ([]UserGroup, error) { return c.ListUserGroups(ctx) },
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
		newListSubcommand[SlackChannel](loader, "slack-channels", "SlackChannel", "List Slack channels.", "id",
			func(ctx context.Context, c OnCallAPI) ([]SlackChannel, error) { return c.ListSlackChannels(ctx) },
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
			func(ctx context.Context, c OnCallAPI, name string) (*Alert, error) { return c.GetAlert(ctx, name) }),
	)
	return cmd
}

func newOrganizationsCmd(loader OnCallConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "organizations",
		Short:   "View organization info.",
		Aliases: []string{"organization", "org"},
	}
	cmd.AddCommand(
		newGetSubcommand(loader, "Get organization info.",
			func(ctx context.Context, c OnCallAPI, _ string) (*Organization, error) {
				return c.GetOrganization(ctx)
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
		newListSubcommand(loader, "resolution-notes", "ResolutionNote", "List resolution notes.", "id",
			func(ctx context.Context, c OnCallAPI) ([]ResolutionNote, error) {
				return c.ListResolutionNotes(ctx, "")
			},
			func(ctx context.Context, c OnCallAPI, name string) (*ResolutionNote, error) {
				return c.GetResolutionNote(ctx, name)
			}),
		newGetSubcommand(loader, "Get a resolution note by ID.",
			func(ctx context.Context, c OnCallAPI, name string) (*ResolutionNote, error) {
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
		newListSubcommand(loader, "shift-swaps", "ShiftSwap", "List shift swaps.", "id",
			func(ctx context.Context, c OnCallAPI) ([]ShiftSwap, error) { return c.ListShiftSwaps(ctx) },
			func(ctx context.Context, c OnCallAPI, name string) (*ShiftSwap, error) {
				return c.GetShiftSwap(ctx, name)
			}),
		newGetSubcommand(loader, "Get a shift swap by ID.",
			func(ctx context.Context, c OnCallAPI, name string) (*ShiftSwap, error) {
				return c.GetShiftSwap(ctx, name)
			}),
	)
	return cmd
}

// ---------------------------------------------------------------------------
// Table codecs — accept []unstructured.Unstructured (Pattern 13 compliant)
// ---------------------------------------------------------------------------

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

// --- Integration codec (internal: verbal_name, integration, team) ---

type integrationTableCodec struct{ Wide bool }

func (c *integrationTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *integrationTableCodec) Encode(w io.Writer, v any) error {
	items, err := toUnstructuredSlice(v)
	if err != nil {
		return err
	}
	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("ID", "NAME", "TYPE", "TEAM", "URL")
	} else {
		t = style.NewTable("ID", "NAME", "TYPE")
	}
	for _, obj := range items {
		id := obj.GetName()
		name := specStr(obj, "verbal_name")
		if !c.Wide {
			name = truncate(name, 50)
		}
		if c.Wide {
			t.Row(id, name, specStr(obj, "integration"), orDash(specStr(obj, "team")), orDash(specStr(obj, "integration_url")))
		} else {
			t.Row(id, name, specStr(obj, "integration"))
		}
	}
	return t.Render(w)
}

func (c *integrationTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// --- EscalationChain codec ---

type escalationChainTableCodec struct{}

func (c *escalationChainTableCodec) Format() format.Format { return "table" }

func (c *escalationChainTableCodec) Encode(w io.Writer, v any) error {
	items, err := toUnstructuredSlice(v)
	if err != nil {
		return err
	}
	t := style.NewTable("ID", "NAME", "TEAM")
	for _, obj := range items {
		t.Row(obj.GetName(), specStr(obj, "name"), orDash(specStr(obj, "team")))
	}
	return t.Render(w)
}

func (c *escalationChainTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// --- EscalationPolicy codec (internal: step, wait_delay, escalation_chain) ---

type escalationPolicyTableCodec struct{ Wide bool }

func (c *escalationPolicyTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *escalationPolicyTableCodec) Encode(w io.Writer, v any) error {
	items, err := toUnstructuredSlice(v)
	if err != nil {
		return err
	}
	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("ID", "CHAIN", "STEP", "WAIT-DELAY", "IMPORTANT", "NOTIFY-SCHEDULE")
	} else {
		t = style.NewTable("ID", "CHAIN", "STEP", "WAIT-DELAY")
	}
	for _, obj := range items {
		id := obj.GetName()
		waitDelay := orDash(specStr(obj, "wait_delay"))
		if c.Wide {
			important := "false"
			if specBool(obj, "important") {
				important = "true"
			}
			t.Row(id, specStr(obj, "escalation_chain"), specStr(obj, "step"), waitDelay, important, orDash(specStr(obj, "notify_schedule")))
		} else {
			t.Row(id, specStr(obj, "escalation_chain"), specStr(obj, "step"), waitDelay)
		}
	}
	return t.Render(w)
}

func (c *escalationPolicyTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// --- Schedule codec ---

type scheduleTableCodec struct{ Wide bool }

func (c *scheduleTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *scheduleTableCodec) Encode(w io.Writer, v any) error {
	items, err := toUnstructuredSlice(v)
	if err != nil {
		return err
	}
	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("ID", "NAME", "TYPE", "TIMEZONE", "TEAM")
	} else {
		t = style.NewTable("ID", "NAME", "TYPE", "TIMEZONE")
	}
	for _, obj := range items {
		id := obj.GetName()
		tz := orDash(specStr(obj, "time_zone"))
		if c.Wide {
			t.Row(id, specStr(obj, "name"), specStr(obj, "type"), tz, orDash(specStr(obj, "team")))
		} else {
			t.Row(id, specStr(obj, "name"), specStr(obj, "type"), tz)
		}
	}
	return t.Render(w)
}

func (c *scheduleTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// --- Shift codec (internal: shift_start, shift_end, priority_level) ---

type shiftTableCodec struct{ Wide bool }

func (c *shiftTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *shiftTableCodec) Encode(w io.Writer, v any) error {
	items, err := toUnstructuredSlice(v)
	if err != nil {
		return err
	}
	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("ID", "NAME", "TYPE", "START", "END", "FREQUENCY", "INTERVAL")
	} else {
		t = style.NewTable("ID", "NAME", "TYPE", "START", "END")
	}
	for _, obj := range items {
		id := obj.GetName()
		start := orDash(specStr(obj, "shift_start"))
		end := orDash(specStr(obj, "shift_end"))
		if c.Wide {
			t.Row(id, specStr(obj, "name"), specStr(obj, "type"), start, end, orDash(specStr(obj, "frequency")), strconv.Itoa(specInt(obj, "interval")))
		} else {
			t.Row(id, specStr(obj, "name"), specStr(obj, "type"), start, end)
		}
	}
	return t.Render(w)
}

func (c *shiftTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// --- Route codec (internal: alert_receive_channel, escalation_chain, filtering_term) ---

type routeTableCodec struct{ Wide bool }

func (c *routeTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *routeTableCodec) Encode(w io.Writer, v any) error {
	items, err := toUnstructuredSlice(v)
	if err != nil {
		return err
	}
	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("ID", "INTEGRATION", "CHAIN", "FILTER-TYPE", "FILTER", "DEFAULT")
	} else {
		t = style.NewTable("ID", "INTEGRATION", "CHAIN", "FILTER-TYPE")
	}
	for _, obj := range items {
		id := obj.GetName()
		if c.Wide {
			isDefault := "false"
			if specBool(obj, "is_default") {
				isDefault = "true"
			}
			filter := orDash(specStr(obj, "filtering_term"))
			if len(filter) > 40 {
				filter = filter[:37] + "..."
			}
			t.Row(id, specStr(obj, "alert_receive_channel"), orDash(specStr(obj, "escalation_chain")), orDash(specStr(obj, "filtering_term_type")), filter, isDefault)
		} else {
			t.Row(id, specStr(obj, "alert_receive_channel"), orDash(specStr(obj, "escalation_chain")), orDash(specStr(obj, "filtering_term_type")))
		}
	}
	return t.Render(w)
}

func (c *routeTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// --- Webhook codec ---

type webhookTableCodec struct{ Wide bool }

func (c *webhookTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *webhookTableCodec) Encode(w io.Writer, v any) error {
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

func (c *webhookTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// --- AlertGroup codec (internal: pk, status, started_at) ---

type alertGroupTableCodec struct{ Wide bool }

func (c *alertGroupTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *alertGroupTableCodec) Encode(w io.Writer, v any) error {
	items, err := toUnstructuredSlice(v)
	if err != nil {
		return err
	}
	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("ID", "STATUS", "ALERTS", "STARTED", "INTEGRATION", "TEAM")
	} else {
		t = style.NewTable("ID", "STATUS", "ALERTS", "STARTED")
	}
	for _, obj := range items {
		id := obj.GetName()
		started := specStr(obj, "started_at")
		if len(started) > 16 {
			started = started[:16]
		}
		started = orDash(started)
		alerts := specInt(obj, "alerts_count")
		status := specStr(obj, "status")
		if c.Wide {
			t.Row(id, status, strconv.Itoa(alerts), started, orDash(specStr(obj, "alert_receive_channel")), orDash(specStr(obj, "team")))
		} else {
			t.Row(id, status, strconv.Itoa(alerts), started)
		}
	}
	return t.Render(w)
}

func (c *alertGroupTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// --- User codec (internal: pk, avatar, current_team) ---

type userTableCodec struct{ Wide bool }

func (c *userTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *userTableCodec) Encode(w io.Writer, v any) error {
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

func (c *userTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// --- Team codec ---

type teamTableCodec struct{}

func (c *teamTableCodec) Format() format.Format { return "table" }

func (c *teamTableCodec) Encode(w io.Writer, v any) error {
	items, err := toUnstructuredSlice(v)
	if err != nil {
		return err
	}
	t := style.NewTable("ID", "NAME", "EMAIL")
	for _, obj := range items {
		t.Row(obj.GetName(), specStr(obj, "name"), orDash(specStr(obj, "email")))
	}
	return t.Render(w)
}

func (c *teamTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// --- UserGroup codec ---

type userGroupTableCodec struct{}

func (c *userGroupTableCodec) Format() format.Format { return "table" }

func (c *userGroupTableCodec) Encode(w io.Writer, v any) error {
	items, err := toUnstructuredSlice(v)
	if err != nil {
		return err
	}
	t := style.NewTable("ID", "NAME", "HANDLE")
	for _, obj := range items {
		t.Row(obj.GetName(), orDash(specStr(obj, "name")), orDash(specStr(obj, "handle")))
	}
	return t.Render(w)
}

func (c *userGroupTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// --- SlackChannel codec ---

type slackChannelTableCodec struct{}

func (c *slackChannelTableCodec) Format() format.Format { return "table" }

func (c *slackChannelTableCodec) Encode(w io.Writer, v any) error {
	items, err := toUnstructuredSlice(v)
	if err != nil {
		return err
	}
	t := style.NewTable("ID", "NAME", "SLACK-ID")
	for _, obj := range items {
		t.Row(obj.GetName(), orDash(specStr(obj, "display_name")), orDash(specStr(obj, "slack_id")))
	}
	return t.Render(w)
}

func (c *slackChannelTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// --- Alert codec ---

type alertTableCodec struct{}

func (c *alertTableCodec) Format() format.Format { return "table" }

func (c *alertTableCodec) Encode(w io.Writer, v any) error {
	items, err := toUnstructuredSlice(v)
	if err != nil {
		return err
	}
	t := style.NewTable("ID", "CREATED")
	for _, obj := range items {
		created := specStr(obj, "created_at")
		if len(created) > 16 {
			created = created[:16]
		}
		t.Row(obj.GetName(), orDash(created))
	}
	return t.Render(w)
}

func (c *alertTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// --- Organization codec ---

type organizationTableCodec struct{}

func (c *organizationTableCodec) Format() format.Format { return "table" }

func (c *organizationTableCodec) Encode(w io.Writer, v any) error {
	items, err := toUnstructuredSlice(v)
	if err != nil {
		return err
	}
	t := style.NewTable("ID", "NAME", "SLUG")
	for _, obj := range items {
		t.Row(obj.GetName(), orDash(specStr(obj, "name")), orDash(specStr(obj, "stack_slug")))
	}
	return t.Render(w)
}

func (c *organizationTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// --- ResolutionNote codec ---

type resolutionNoteTableCodec struct{ Wide bool }

func (c *resolutionNoteTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *resolutionNoteTableCodec) Encode(w io.Writer, v any) error {
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
			t.Row(obj.GetName(), specStr(obj, "alert_group"), orDash(specStr(obj, "source")), orDash(created), orDash(text))
		} else {
			t.Row(obj.GetName(), specStr(obj, "alert_group"), orDash(specStr(obj, "source")), orDash(created))
		}
	}
	return t.Render(w)
}

func (c *resolutionNoteTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// --- ShiftSwap codec ---

type shiftSwapTableCodec struct{ Wide bool }

func (c *shiftSwapTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *shiftSwapTableCodec) Encode(w io.Writer, v any) error {
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

func (c *shiftSwapTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}
