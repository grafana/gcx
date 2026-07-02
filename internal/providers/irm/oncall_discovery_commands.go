package irm

import (
	"context"
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// errInvalidTableInput is returned by discovery table codecs when Encode
// receives a value of an unexpected type.
var errInvalidTableInput = errors.New("invalid data type for table codec")

// Discovery commands surface the enum catalogs behind OnCall resource fields
// that previously required raw `gcx api` calls or hard-coded numeric "magic
// values" (e.g. escalation step 19 for declare-incident, webhook trigger_type
// 12 for incident-changed, route filtering_term_type 0 for regex). All
// catalogs are fetched live from the IRM backend.

type discoveryListOpts struct {
	IO cmdio.Options
}

func (o *discoveryListOpts) setup(flags *pflag.FlagSet, codec format.Codec) {
	o.IO.RegisterCustomCodec("table", codec)
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

// newDiscoveryListCmd builds a `list` command that fetches a catalog via
// fetch and emits it through the codec system.
func newDiscoveryListCmd[T any](
	loader OnCallConfigLoader, short string, codec format.Codec,
	fetch func(ctx context.Context, client OnCallAPI) ([]T, error),
) *cobra.Command {
	opts := &discoveryListOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			client, _, err := loader.LoadOnCallClient(ctx)
			if err != nil {
				return err
			}
			items, err := fetch(ctx, client)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), items)
		},
	}
	opts.setup(cmd.Flags(), codec)
	return cmd
}

// newEscalationStepsCmd returns the `escalation-policies steps` command tree.
func newEscalationStepsCmd(loader OnCallConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "steps",
		Short: "Discover allowed escalation policy step types.",
	}
	cmd.AddCommand(newDiscoveryListCmd(loader,
		"List allowed values for an escalation policy's step field.",
		&escalationStepOptionTableCodec{},
		func(ctx context.Context, client OnCallAPI) ([]EscalationStepOption, error) {
			return client.ListEscalationStepOptions(ctx)
		}))
	return cmd
}

// newWebhookTriggersCmd returns the `webhooks triggers` command tree.
func newWebhookTriggersCmd(loader OnCallConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "triggers",
		Short: "Discover allowed webhook trigger types.",
	}
	cmd.AddCommand(newDiscoveryListCmd(loader,
		"List allowed values for a webhook's trigger_type field.",
		&webhookTriggerOptionTableCodec{},
		func(ctx context.Context, client OnCallAPI) ([]WebhookTriggerOption, error) {
			return client.ListWebhookTriggerOptions(ctx)
		}))
	return cmd
}

// newWebhookPresetsCmd returns the `webhooks presets` command tree.
func newWebhookPresetsCmd(loader OnCallConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "presets",
		Short: "Discover webhook configuration presets.",
	}
	cmd.AddCommand(newDiscoveryListCmd(loader,
		"List webhook preset IDs (e.g. grafana_assistant) and their allowed triggers.",
		&webhookPresetTableCodec{},
		func(ctx context.Context, client OnCallAPI) ([]WebhookPreset, error) {
			return client.ListWebhookPresets(ctx)
		}))
	return cmd
}

// newRouteFilterTypesCmd returns the `routes filter-types` command tree.
func newRouteFilterTypesCmd(loader OnCallConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "filter-types",
		Short: "Discover route filtering term types.",
	}
	cmd.AddCommand(newDiscoveryListCmd(loader,
		"List allowed values for a route's filtering_term_type field.",
		&routeFilterTypeTableCodec{},
		func(ctx context.Context, client OnCallAPI) ([]RouteFilterType, error) {
			return client.ListRouteFilterTypes(ctx)
		}))
	return cmd
}

// --- Table codecs ---

type escalationStepOptionTableCodec struct{ noDecodeCodec }

func (c *escalationStepOptionTableCodec) Format() format.Format { return "table" }

func (c *escalationStepOptionTableCodec) Encode(w io.Writer, v any) error {
	items, ok := v.([]EscalationStepOption)
	if !ok {
		return errInvalidTableInput
	}
	t := style.NewTable("VALUE", "NAME", "DISPLAY NAME")
	for _, it := range items {
		t.Row(strconv.Itoa(it.Value), it.CreateDisplayName, it.DisplayName)
	}
	return t.Render(w)
}

type webhookTriggerOptionTableCodec struct{ noDecodeCodec }

func (c *webhookTriggerOptionTableCodec) Format() format.Format { return "table" }

func (c *webhookTriggerOptionTableCodec) Encode(w io.Writer, v any) error {
	items, ok := v.([]WebhookTriggerOption)
	if !ok {
		return errInvalidTableInput
	}
	t := style.NewTable("VALUE", "NAME")
	for _, it := range items {
		t.Row(strconv.Itoa(it.Value), it.DisplayName)
	}
	return t.Render(w)
}

type webhookPresetTableCodec struct{ noDecodeCodec }

func (c *webhookPresetTableCodec) Format() format.Format { return "table" }

func (c *webhookPresetTableCodec) Encode(w io.Writer, v any) error {
	items, ok := v.([]WebhookPreset)
	if !ok {
		return errInvalidTableInput
	}
	t := style.NewTable("ID", "NAME", "TRIGGERS", "DESCRIPTION")
	for _, it := range items {
		triggers := make([]string, 0, len(it.TriggerTypes))
		for _, tt := range it.TriggerTypes {
			triggers = append(triggers, tt.Value)
		}
		t.Row(it.ID, it.Name, strings.Join(triggers, ", "), it.Description)
	}
	return t.Render(w)
}

type routeFilterTypeTableCodec struct{ noDecodeCodec }

func (c *routeFilterTypeTableCodec) Format() format.Format { return "table" }

func (c *routeFilterTypeTableCodec) Encode(w io.Writer, v any) error {
	items, ok := v.([]RouteFilterType)
	if !ok {
		return errInvalidTableInput
	}
	t := style.NewTable("VALUE", "NAME")
	for _, it := range items {
		t.Row(strconv.Itoa(it.Value), it.DisplayName)
	}
	return t.Render(w)
}
