package irm

import (
	"io"
	"strconv"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Discovery commands surface enum catalogs that previously required raw
// `gcx api` calls or hard-coded numeric "magic values" (e.g. escalation step
// 19 for declare-incident, webhook trigger_type 12 for incident-changed,
// route filtering_term_type 0 for regex).

// staticWebhookTriggerOptions is the catalog of webhook trigger_type values
// recognized by the IRM backend. Sourced from the OnCall data model; kept
// static because no public discovery endpoint surfaces this enum.
//
//nolint:gochecknoglobals // intentional static catalog
var staticWebhookTriggerOptions = []WebhookTriggerOption{
	{Value: 0, Name: "escalation", DisplayName: "Escalation step"},
	{Value: 1, Name: "alert group created", DisplayName: "Alert Group Created"},
	{Value: 2, Name: "acknowledge", DisplayName: "Acknowledged"},
	{Value: 3, Name: "resolve", DisplayName: "Resolved"},
	{Value: 4, Name: "silence", DisplayName: "Silenced"},
	{Value: 5, Name: "unsilence", DisplayName: "Unsilenced"},
	{Value: 6, Name: "unresolve", DisplayName: "Unresolved"},
	{Value: 7, Name: "unacknowledge", DisplayName: "Unacknowledged"},
	{Value: 12, Name: "incident changed", DisplayName: "Incident Changed"},
}

// staticWebhookPresetOptions is the catalog of known preset IDs. Static
// because no public discovery endpoint surfaces this enum.
//
//nolint:gochecknoglobals // intentional static catalog
var staticWebhookPresetOptions = []WebhookPresetOption{
	{ID: "grafana_assistant", Name: "Grafana Assistant", Description: "Forward alert groups to a Grafana Assistant investigation."},
	{ID: "advanced_webhook", Name: "Advanced Webhook", Description: "Fully configurable outgoing webhook."},
	{ID: "simple_webhook", Name: "Simple Webhook", Description: "Minimal POST with a fixed payload."},
}

// staticRouteFilterTypes is the catalog of allowed route filtering_term_type
// values. The IRM backend defines three filter types; no discovery endpoint
// surfaces them.
//
//nolint:gochecknoglobals // intentional static catalog
var staticRouteFilterTypes = []RouteFilterType{
	{Value: 0, Name: "regex", DisplayName: "Regular expression"},
	{Value: 1, Name: "jinja2", DisplayName: "Jinja2 template"},
	{Value: 2, Name: "labels", DisplayName: "Label match"},
}

type discoveryListOpts struct {
	IO cmdio.Options
}

func (o *discoveryListOpts) setup(flags *pflag.FlagSet, codec format.Codec) {
	o.IO.RegisterCustomCodec("table", codec)
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

// newEscalationStepsCmd returns the `escalation-policies steps` command tree.
func newEscalationStepsCmd(loader OnCallConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "steps",
		Short: "Discover allowed escalation policy step types.",
	}
	cmd.AddCommand(newEscalationStepsListCmd(loader))
	return cmd
}

func newEscalationStepsListCmd(loader OnCallConfigLoader) *cobra.Command {
	opts := &discoveryListOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List allowed values for an escalation policy's step field.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			client, _, err := loader.LoadOnCallClient(ctx)
			if err != nil {
				return err
			}
			items, err := client.ListEscalationStepOptions(ctx)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), items)
		},
	}
	opts.setup(cmd.Flags(), &escalationStepOptionTableCodec{})
	return cmd
}

// newWebhookTriggersCmd returns the `webhooks triggers` command tree.
func newWebhookTriggersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "triggers",
		Short: "Discover allowed webhook trigger types.",
	}
	cmd.AddCommand(newWebhookTriggersListCmd())
	return cmd
}

func newWebhookTriggersListCmd() *cobra.Command {
	opts := &discoveryListOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List allowed values for a webhook's trigger_type field.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), staticWebhookTriggerOptions)
		},
	}
	opts.setup(cmd.Flags(), &webhookTriggerOptionTableCodec{})
	return cmd
}

// newWebhookPresetsCmd returns the `webhooks presets` command tree.
func newWebhookPresetsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "presets",
		Short: "Discover webhook configuration presets.",
	}
	cmd.AddCommand(newWebhookPresetsListCmd())
	return cmd
}

func newWebhookPresetsListCmd() *cobra.Command {
	opts := &discoveryListOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List known webhook preset IDs (e.g. grafana_assistant).",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), staticWebhookPresetOptions)
		},
	}
	opts.setup(cmd.Flags(), &webhookPresetOptionTableCodec{})
	return cmd
}

// newRouteFilterTypesCmd returns the `routes filter-types` command tree.
func newRouteFilterTypesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "filter-types",
		Short: "Discover route filtering term types.",
	}
	cmd.AddCommand(newRouteFilterTypesListCmd())
	return cmd
}

func newRouteFilterTypesListCmd() *cobra.Command {
	opts := &discoveryListOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List allowed values for a route's filtering_term_type field.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), staticRouteFilterTypes)
		},
	}
	opts.setup(cmd.Flags(), &routeFilterTypeTableCodec{})
	return cmd
}

// --- Table codecs ---

type escalationStepOptionTableCodec struct{ noDecodeCodec }

func (escalationStepOptionTableCodec) Format() format.Format { return "table" }

func (escalationStepOptionTableCodec) Encode(w io.Writer, v any) error {
	items, _ := v.([]EscalationStepOption)
	t := style.NewTable("VALUE", "NAME", "DISPLAY NAME")
	for _, it := range items {
		t.Row(strconv.Itoa(it.Value), it.CreateDisplayName, it.DisplayName)
	}
	return t.Render(w)
}

type webhookTriggerOptionTableCodec struct{ noDecodeCodec }

func (webhookTriggerOptionTableCodec) Format() format.Format { return "table" }

func (webhookTriggerOptionTableCodec) Encode(w io.Writer, v any) error {
	items, _ := v.([]WebhookTriggerOption)
	t := style.NewTable("VALUE", "NAME", "DISPLAY NAME")
	for _, it := range items {
		t.Row(strconv.Itoa(it.Value), it.Name, it.DisplayName)
	}
	return t.Render(w)
}

type webhookPresetOptionTableCodec struct{ noDecodeCodec }

func (webhookPresetOptionTableCodec) Format() format.Format { return "table" }

func (webhookPresetOptionTableCodec) Encode(w io.Writer, v any) error {
	items, _ := v.([]WebhookPresetOption)
	t := style.NewTable("ID", "NAME", "DESCRIPTION")
	for _, it := range items {
		t.Row(it.ID, it.Name, it.Description)
	}
	return t.Render(w)
}

type routeFilterTypeTableCodec struct{ noDecodeCodec }

func (routeFilterTypeTableCodec) Format() format.Format { return "table" }

func (routeFilterTypeTableCodec) Encode(w io.Writer, v any) error {
	items, _ := v.([]RouteFilterType)
	t := style.NewTable("VALUE", "NAME", "DISPLAY NAME")
	for _, it := range items {
		t.Row(strconv.Itoa(it.Value), it.Name, it.DisplayName)
	}
	return t.Render(w)
}
