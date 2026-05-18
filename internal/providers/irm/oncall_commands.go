package irm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/style"
	"github.com/grafana/gcx/internal/terminal"
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
	// Default codec name for CRUD list/get/list-alerts. The alert-groups
	// command tree defaults to `text` (table); other OnCall resources
	// continue to default to `table` per the existing convention.
	defaultFmt := "table"
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
		// alert-groups list registers a `table` codec — uniform with the
		// CRUD-data-command default model in CONSTITUTION/DESIGN.
		o.IO.RegisterCustomCodec("table", &alertGroupTableCodec{})
		o.IO.RegisterCustomCodec("wide", &alertGroupTableCodec{Wide: true})
		defaultFmt = "table"
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
		// Same table-codec default applies to `alert-groups list-alerts <group-id>`,
		// which dispatches via the "alerts" case here.
		o.IO.RegisterCustomCodec("table", &alertTableCodec{})
		o.IO.RegisterCustomCodec("wide", &alertTableCodec{Wide: true})
		defaultFmt = "table"
	case "organizations":
		o.IO.RegisterCustomCodec("table", &organizationTableCodec{})
	case "resolution-notes":
		o.IO.RegisterCustomCodec("table", &resolutionNoteTableCodec{})
		o.IO.RegisterCustomCodec("wide", &resolutionNoteTableCodec{Wide: true})
	case "shift-swaps":
		o.IO.RegisterCustomCodec("table", &shiftSwapTableCodec{})
		o.IO.RegisterCustomCodec("wide", &shiftSwapTableCodec{Wide: true})
	}
	o.IO.DefaultFormat(defaultFmt)
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

			specs := make([]T, len(typedObjs))
			for i, obj := range typedObjs {
				specs[i] = obj.Spec
			}
			objs, err := itemsToUnstructured(specs, kind, idField, namespace)
			if err != nil {
				return err
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
		StripFields: DefaultStripFields,
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

func newOrganizationsCmd(loader OnCallConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "organizations",
		Short:   "View organization info.",
		Aliases: []string{"organization", "org"},
	}
	opts := &getOpts{}
	getCmd := &cobra.Command{
		Use:   "get",
		Short: "Get organization info.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, _, err := loader.LoadOnCallClient(cmd.Context())
			if err != nil {
				return err
			}
			org, err := client.GetOrganization(cmd.Context())
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), org)
		},
	}
	opts.setup(getCmd.Flags())
	cmd.AddCommand(getCmd)
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

// noDecodeCodec is embedded in all table codecs to provide the shared
// Decode stub — table format is output-only.
type noDecodeCodec struct{}

func (noDecodeCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

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

type integrationTableCodec struct {
	noDecodeCodec

	Wide bool
}

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

// --- EscalationChain codec ---

type escalationChainTableCodec struct{ noDecodeCodec }

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

// --- EscalationPolicy codec (internal: step, wait_delay, escalation_chain) ---

type escalationPolicyTableCodec struct {
	noDecodeCodec

	Wide bool
}

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

// --- Schedule codec ---

type scheduleTableCodec struct {
	noDecodeCodec

	Wide bool
}

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

// --- Shift codec (internal: shift_start, shift_end, priority_level) ---

type shiftTableCodec struct {
	noDecodeCodec

	Wide bool
}

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

// --- Route codec (internal: alert_receive_channel, escalation_chain, filtering_term) ---

type routeTableCodec struct {
	noDecodeCodec

	Wide bool
}

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

// --- Webhook codec ---

type webhookTableCodec struct {
	noDecodeCodec

	Wide bool
}

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

// --- AlertGroup codec (typed: accepts []alertGroupEnvelope) ---
//
// The list path emits typed envelopes (not unstructured.Unstructured) so JSON
// and YAML output preserve the deliberate field order under status (title,
// summary, severity, state, runbookURL, subject, timestamps, links,
// alertsCount, raw) defined on oncalltypes.AlertGroupStatus.
// The table codec below mirrors the locked SRE-persona column shape for
// `irm oncall alert-groups list` (see ADR §6).

// SubjectLabelPriority is the column-rendering priority for AlertGroup
// subject labels: keys present in this list are picked first (in order);
// remaining keys fall back to alphabetical order.
//
//nolint:gochecknoglobals // stable shape, configured per ADR §6.
var SubjectLabelPriority = []string{
	"service",
	"job",
	"workload",
	"app",
	"deployment",
	"component",
	"namespace",
	"cluster",
	"container",
}

// DimensionsLabelPriority is the column-rendering priority for per-Alert
// dimensions labels (`pod` is the dominant single-row discriminator).
//
//nolint:gochecknoglobals // stable shape, configured per ADR §6.
var DimensionsLabelPriority = []string{
	"pod",
	"instance",
	"tenant",
	"slug",
	"name",
	"container",
	"deployment",
}

// pickLabelByPriority returns the (key, value) pair whose key has the
// highest-priority match in `priority` (earliest occurrence wins). When no
// priority key is present in `labels`, falls back to the alphabetically
// first key. Returns ("", "") when `labels` is empty.
func pickLabelByPriority(labels map[string]string, priority []string) (string, string) {
	if len(labels) == 0 {
		return "", ""
	}
	for _, k := range priority {
		if v, ok := labels[k]; ok {
			return k, v
		}
	}
	// Alphabetical fallback.
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return "", ""
	}
	return keys[0], labels[keys[0]]
}

// RenderLabelCell renders a labels map as a single-line table cell:
//
//	"key=value (+N)"
//
// The picked key follows `priority`-then-alphabetical order; `(+N)` is the
// count of OTHER non-noise labels not shown. The result is run-truncated to
// `budget` runes (via truncateRunes); pass 0 for no truncation. Returns
// "-" when the input is empty or all labels are denylisted.
//
// Note: the input is expected to be already filtered (see filterLabels) on
// the data-model side; this helper applies the denylist defensively so it
// can be called with raw maps too.
func RenderLabelCell(labels map[string]string, priority []string, budget int) string {
	filtered := filterLabels(labels)
	if len(filtered) == 0 {
		return "-"
	}
	k, v := pickLabelByPriority(filtered, priority)
	if k == "" {
		return "-"
	}
	rendered := k + "=" + v
	remaining := len(filtered) - 1
	if remaining > 0 {
		rendered = fmt.Sprintf("%s (+%d)", rendered, remaining)
	}
	return truncateRunes(rendered, budget)
}

// formatTeamCell renders the TEAM column cell as `<name> (<id>)` while
// preserving the team ID intact when the cell overflows the available
// budget. Team IDs are needed for copy-paste into other gcx commands
// (e.g. `--team-id=...`), so the truncation point falls on the NAME, not
// the ID.
//
// Width regimes:
//
//  1. budget <= 0 OR `<name> (<id>)` fits in budget → render verbatim.
//  2. budget tight, name+id+wrapper overflows → truncate the name, keep the
//     full id: `<name-truncated…> (<id>)`. The minimum visible name segment
//     is one rune plus the ellipsis (so `n… (<id>)`).
//  3. budget so tight that even `… (<id>)` doesn't fit → fall back to
//     `(<id>)` alone (no name) so the ID is always preserved. Last-resort
//     for narrow terminals.
//
// Empty inputs return "-" (when both name and id are empty) or the id alone
// when only the id is set, mirroring the pre-existing `<name> (<id>)` shape.
func formatTeamCell(name, id string, budget int) string {
	if name == "" && id == "" {
		return "-"
	}
	if name == "" {
		// Pre-existing shape was `<name> (<id>)` with name="" rendering as
		// ` (<id>)`; keep parity by stripping the leading space and just
		// emitting `(<id>)`.
		return "(" + id + ")"
	}
	if id == "" {
		// No ID to preserve — fall back to name truncation.
		return truncateRunes(name, budget)
	}

	full := name + " (" + id + ")"
	if budget <= 0 || utf8.RuneCountInString(full) <= budget {
		return full
	}

	// id-with-wrapper takes `(<id>)` runes — that's len(id)+2.
	idRunes := utf8.RuneCountInString(id)
	wrapperRunes := idRunes + 2
	// Reserve at least 1 rune for ellipsis + 1 space separator before
	// `(<id>)` (so total = 2 fixed + idRunes overhead).
	const minNameWithEllipsis = 1 // smallest meaningful name slice rendered

	// nameBudget = budget - (space + wrapperRunes). The truncated name
	// will end in '…' (consumes 1 rune from the name budget).
	nameBudget := budget - wrapperRunes - 1 // subtract space separator
	if nameBudget < minNameWithEllipsis+1 {
		// Even `… (<id>)` doesn't fit — last resort: drop the name entirely
		// to preserve the ID.
		return "(" + id + ")"
	}
	truncatedName := truncateRunes(name, nameBudget)
	return truncatedName + " (" + id + ")"
}

// renderLabelCellMultiLine renders a labels map as a multi-line table cell:
// one `key=value` per line, in priority-then-alphabetical order, denylisted
// keys filtered out. Used by the wide-mode SUBJECT/DIMENSIONS column. Returns
// "-" when the input is empty.
//
// Newlines are flattened to ", " in the plain (tabwriter) renderer path —
// see `style.TableBuilder.renderPlain`.
func renderLabelCellMultiLine(labels map[string]string, priority []string) string {
	filtered := filterLabels(labels)
	if len(filtered) == 0 {
		return "-"
	}
	// Build ordered key list: priority keys first (in order), then
	// remaining keys alphabetically.
	seen := map[string]bool{}
	ordered := make([]string, 0, len(filtered))
	for _, k := range priority {
		if _, ok := filtered[k]; ok && !seen[k] {
			ordered = append(ordered, k)
			seen[k] = true
		}
	}
	rest := make([]string, 0, len(filtered))
	for k := range filtered {
		if !seen[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	ordered = append(ordered, rest...)
	parts := make([]string, 0, len(ordered))
	for _, k := range ordered {
		parts = append(parts, k+"="+filtered[k])
	}
	return strings.Join(parts, "\n")
}

// alertGroupRuleCell returns the RULE cell for the wide alertGroup table: the
// rule URL takes precedence, then the UID, then "-". Extracted from the codec's
// row loop to reduce nesting depth (nestif).
func alertGroupRuleCell(env alertGroupEnvelope) string {
	if env.Status.Links == nil || env.Status.Links.Alert == nil || env.Status.Links.Alert.Rule == nil {
		return "-"
	}
	if u := env.Status.Links.Alert.Rule.URL; u != "" {
		return u
	}
	if uid := env.Status.Links.Alert.Rule.UID; uid != "" {
		return uid
	}
	return "-"
}

type alertGroupTableCodec struct {
	noDecodeCodec

	Wide bool
}

func (c *alertGroupTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *alertGroupTableCodec) Encode(w io.Writer, v any) error {
	// Accept three shapes:
	//   - `alertGroupItemsEnvelope` — what `alert-groups list` passes
	//     (JSON shape `{"items": [...]}`; the table view renders the items
	//     slice as rows).
	//   - `[]alertGroupEnvelope` — back-compat for callers that pass the
	//     slice directly (kept for tests and the legacy SA-token path).
	//   - `alertGroupEnvelope` — what `alert-groups get` passes; renders as
	//     a single header + data row.
	var envs []alertGroupEnvelope
	switch x := v.(type) {
	case alertGroupItemsEnvelope:
		envs = x.Items
	case []alertGroupEnvelope:
		envs = x
	case alertGroupEnvelope:
		envs = []alertGroupEnvelope{x}
	default:
		return errors.New("invalid data type for table codec: expected alertGroupItemsEnvelope, []alertGroupEnvelope, or alertGroupEnvelope")
	}
	// Locked column widths. Per-column values include lipgloss padding (the
	// styled renderer applies Padding(0,1), which lipgloss counts inside
	// Width()): a 16-wide ID column has 14 chars of content room, enough
	// for a 13-char OnCall PK plus headroom.
	var (
		t              *style.TableBuilder
		colWidths      []int
		fixedColsWidth int
	)
	if c.Wide {
		// Wide layout (ADR §6): ID, TITLE, TEAM, SEVERITY, STATE, RULE, SUBJECT, ALERTS, AGE
		t = style.NewTable("ID", "TITLE", "TEAM", "SEVERITY", "STATE", "RULE", "SUBJECT", "ALERTS", "AGE")
		colWidths = []int{16, 0, 0, 10, 14, 0, 0, 8, 12}
		t.MultilineCells(true) // SUBJECT cell embeds newlines in wide mode.
	} else {
		// Default layout (ADR §6): ID, TITLE, SEVERITY, STATE, TEAM, SUBJECT, AGE
		t = style.NewTable("ID", "TITLE", "SEVERITY", "STATE", "TEAM", "SUBJECT", "AGE")
		colWidths = []int{16, 0, 10, 14, 0, 0, 12}
	}
	t.ColumnWidths(colWidths)
	for _, w := range colWidths {
		fixedColsWidth += w
	}
	titleBudget := titleAvailableWidth(len(colWidths), fixedColsWidth)
	for _, env := range envs {
		id := env.Metadata.Name
		title := truncateRunes(orDash(env.Status.Title), titleBudget)
		severity := orDash(env.Status.Severity)
		state := orDash(env.Status.State)
		teamCell := "-"
		if env.Spec.Team != nil {
			// Reuse the title budget as the TEAM column budget — both are
			// auto-sized columns in the alertGroup table layout, so they
			// share the same overflow regime.
			teamCell = formatTeamCell(env.Spec.Team.Name, env.Spec.Team.ID, titleBudget)
		}
		age := formatRelativeAge(env.Metadata.CreationTimestamp)
		subjectLabels := map[string]string{}
		if env.Status.Subject != nil {
			subjectLabels = env.Status.Subject.Labels
		}
		if c.Wide {
			t.Row(id, title, teamCell, severity, state,
				alertGroupRuleCell(env), renderLabelCellMultiLine(subjectLabels, SubjectLabelPriority),
				strconv.Itoa(env.Status.AlertsCount), age)
		} else {
			t.Row(id, title, severity, state, teamCell,
				RenderLabelCell(subjectLabels, SubjectLabelPriority, titleBudget), age)
		}
	}
	return t.Render(w)
}

// titleAvailableWidth computes the column budget left for the flexible TITLE
// cell after subtracting the sum of locked-width columns, per-column lipgloss
// borders, and a small safety margin. Returns 0 when the terminal width is
// unknown (truncateRunes treats 0 as "no truncation"), so piped output and
// agent-mode renderings (both fall through the plain tabwriter path) keep the
// full title intact.
func titleAvailableWidth(nCols, fixedColsWidth int) int {
	w := terminal.StdoutWidth()
	if w <= 0 {
		return 0
	}
	// lipgloss table draws nCols+1 vertical borders (1 char each) between
	// cells. Auto-sized columns (count > 0) need at least 1 column each; we
	// over-subtract conservatively so terminal noise (resize races, tab
	// rounding) doesn't push us into wraps.
	autoCols := 0
	if fixedColsWidth > 0 {
		// nCols includes the title; reserve 4 chars for each non-title
		// auto-sized column (TEAM, INTEGRATION, TARGET.CLUSTER, ...).
		autoCols = countAutoCols(nCols, fixedColsWidth)
	}
	const minAutoCol = 4
	const safetyMargin = 4
	budget := w - fixedColsWidth - (nCols + 1) - autoCols*minAutoCol - safetyMargin
	if budget < minTitleWidth {
		return minTitleWidth
	}
	return budget
}

// countAutoCols returns the number of auto-sized (zero-width) columns in the
// codec layout, derived from nCols and the sum of fixed widths. Used by
// titleAvailableWidth to reserve a minimum width per auto-sized column when
// computing the title cell's budget.
//
// The actual count varies per layout (5 in wide mode: TITLE+TEAM+INTEGRATION+
// TARGET.CLUSTER+TARGET.SERVICE+LINKS.SLO.NAME, 1 in narrow mode: TITLE+TEAM).
// We approximate via nCols minus the count of explicitly non-zero entries —
// this is a structural property of the locked column shape, not data-driven,
// so a fixed-width-count override would belong here if the locked shape
// becomes more dynamic in the future.
func countAutoCols(nCols, _ int) int {
	// Conservative: assume half the columns are auto-sized minus the title.
	// In practice this evaluates to:
	//   - narrow (6 cols): 6/2 = 3 → over-reserves but keeps title finite
	//   - wide  (11 cols): 11/2 = 5 → matches the 5 auto-sized columns
	// The over-reservation in narrow mode is harmless (title is the only
	// long column there).
	return max(nCols/2, 1)
}

// minTitleWidth is the floor under which we don't truncate further — below
// this, even an ellipsis-truncated title is uninformative, so we let the table
// renderer wrap rather than emit "…" alone.
const minTitleWidth = 16

// truncateRunes returns s if its rune count is at most width, else returns the
// first (width-1) runes followed by '…'. width <= 0 is treated as "no
// truncation" so callers that pass 0 (e.g. when the terminal width is unknown)
// preserve the full title.
func truncateRunes(s string, width int) string {
	if width <= 0 {
		return s
	}
	if utf8.RuneCountInString(s) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}
	out := make([]rune, 0, width)
	count := 0
	for _, r := range s {
		if count >= width-1 {
			break
		}
		out = append(out, r)
		count++
	}
	out = append(out, '…')
	return string(out)
}

// --- User codec (internal: pk, avatar, current_team) ---

type userTableCodec struct {
	noDecodeCodec

	Wide bool
}

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

// --- Team codec ---

type teamTableCodec struct{ noDecodeCodec }

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

// --- UserGroup codec ---

type userGroupTableCodec struct{ noDecodeCodec }

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

// --- SlackChannel codec ---

type slackChannelTableCodec struct{ noDecodeCodec }

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

// --- Alert codec (typed: accepts []alertEnvelope) ---
//
// Emitted by `irm oncall alert-groups list-alerts <id>`. Locked column set
// (ADR §6 + brief): default = ID, STATE, DIMENSIONS, COUNT.
// Wide adds RULE before DIMENSIONS. DIMENSIONS is the per-alert label diff
// vs the parent group's commonLabels; under default collapse mode rows
// represent unique label sets and COUNT is the count of stored alerts that
// mapped to each row (= status.occurrences in the rich envelope). With
// `--history`, COUNT is always 1 and every stored alert renders on its own
// row.
//
// AGE is intentionally absent from the table render: alertAPI.created_at is
// empty in the OnCall response (pre-existing upstream gap), so AGE always
// rendered "-" and added visual noise. status.metadata.creationTimestamp
// stays in the K8s envelope; only the table column is dropped.
//
// JSON/YAML emission is unchanged — the typed envelope carries the full
// status.{dimensions,occurrences,links,...} block.

type alertTableCodec struct {
	noDecodeCodec

	Wide bool
}

func (c *alertTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

// alertRuleCell renders the single-line RULE cell: URL takes precedence,
// falling back to UID, then "-".
func alertRuleCell(env alertEnvelope) string {
	if env.Status.Links == nil || env.Status.Links.Alert == nil || env.Status.Links.Alert.Rule == nil {
		return "-"
	}
	r := env.Status.Links.Alert.Rule
	if r.URL != "" {
		return r.URL
	}
	if r.UID != "" {
		return r.UID
	}
	return "-"
}

func (c *alertTableCodec) Encode(w io.Writer, v any) error {
	// Accept the items envelope for `alert-groups list-alerts` plus
	// the bare slice form (back-compat for tests / legacy callers).
	var envs []alertEnvelope
	switch x := v.(type) {
	case alertItemsEnvelope:
		envs = x.Items
	case []alertEnvelope:
		envs = x
	default:
		return errors.New("invalid data type for table codec: expected alertItemsEnvelope or []alertEnvelope")
	}
	var (
		t              *style.TableBuilder
		colWidths      []int
		fixedColsWidth int
	)
	// Width semantics match alertGroupTableCodec (lipgloss Padding(0,1) frame
	// included in Width()): a 16-wide ID column holds a 13-char OnCall PK
	// plus headroom. COUNT is fixed-width on the right (5-char header
	// "COUNT" + padding fits comfortably in 8). DIMENSIONS is flex on default
	// and flex/multi-line on wide.
	if c.Wide {
		// Wide layout: ID, STATE, RULE, DIMENSIONS, COUNT
		t = style.NewTable("ID", "STATE", "RULE", "DIMENSIONS", "COUNT")
		colWidths = []int{16, 14, 0, 0, 8}
		t.MultilineCells(true) // DIMENSIONS cell embeds newlines in wide mode.
	} else {
		// Default layout: ID, STATE, DIMENSIONS, COUNT
		t = style.NewTable("ID", "STATE", "DIMENSIONS", "COUNT")
		colWidths = []int{16, 14, 0, 8}
	}
	t.ColumnWidths(colWidths)
	for _, w := range colWidths {
		fixedColsWidth += w
	}
	dimsBudget := titleAvailableWidth(len(colWidths), fixedColsWidth)
	for _, env := range envs {
		id := env.Metadata.Name
		state := orDash(env.Status.State)
		dimLabels := map[string]string{}
		if env.Status.Dimensions != nil {
			dimLabels = env.Status.Dimensions.Labels
		}
		occurrences := env.Status.Occurrences
		if occurrences <= 0 {
			occurrences = 1
		}
		if c.Wide {
			dims := renderLabelCellMultiLine(dimLabels, DimensionsLabelPriority)
			t.Row(id, state, alertRuleCell(env), dims, strconv.Itoa(occurrences))
		} else {
			dims := RenderLabelCell(dimLabels, DimensionsLabelPriority, dimsBudget)
			t.Row(id, state, dims, strconv.Itoa(occurrences))
		}
	}
	return t.Render(w)
}

// --- Organization codec ---

type organizationTableCodec struct{ noDecodeCodec }

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

// --- ResolutionNote codec ---

type resolutionNoteTableCodec struct {
	noDecodeCodec

	Wide bool
}

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

// --- ShiftSwap codec ---

type shiftSwapTableCodec struct {
	noDecodeCodec

	Wide bool
}

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
