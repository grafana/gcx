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

// discoveryCatalog holds one catalog and the help text of its two
// invocations. The struct keeps each text next to the field that names its
// purpose, because the builder takes several strings of the same type.
type discoveryCatalog[T any] struct {
	// command is the canonical compound command, for example
	// "list-step-types".
	command string
	// noun is the older noun group, for example "steps".
	noun string
	// groupShort is the one-line help of the older noun group.
	groupShort string
	// short is the one-line help of the catalog command.
	short string
	codec format.Codec
	fetch func(ctx context.Context, client OnCallAPI) ([]T, error)
}

// newListCmd builds a command that fetches the catalog and emits it through
// the codec system. Each call makes its own opts and binds them to the new
// command's local flag set, so the two commands of one catalog never share
// flag state.
func (c discoveryCatalog[T]) newListCmd(loader OnCallConfigLoader, use, short string) *cobra.Command {
	opts := &discoveryListOpts{}
	cmd := &cobra.Command{
		Use:   use,
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
			items, err := c.fetch(ctx, client)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), items)
		},
	}
	opts.setup(cmd.Flags(), c.codec)
	return cmd
}

// newDiscoveryCmds builds the two invocations that list one catalog:
//
//   - the canonical `list-<subject>` compound command. A catalog facet has no
//     independently addressable item, so the compound spelling keeps the
//     $AREA $NOUN $VERB grammar;
//   - the older `<subject> list` noun group. It shipped, so it stays.
//
// Both invocations come from newListCmd, so they share one implementation.
// The noun group itself stays non-runnable.
func newDiscoveryCmds[T any](loader OnCallConfigLoader, c discoveryCatalog[T]) []*cobra.Command {
	group := &cobra.Command{
		Use:   c.noun,
		Short: c.groupShort,
	}
	group.AddCommand(c.newListCmd(loader, "list", c.short))

	return []*cobra.Command{
		c.newListCmd(loader, c.command, c.short),
		group,
	}
}

// newEscalationStepCmds returns `escalation-policies list-step-types` and the
// older `escalation-policies steps` noun group.
func newEscalationStepCmds(loader OnCallConfigLoader) []*cobra.Command {
	return newDiscoveryCmds(loader, discoveryCatalog[EscalationStepOption]{
		command:    "list-step-types",
		noun:       "steps",
		groupShort: "Discover allowed escalation policy step types.",
		short:      "List allowed values for an escalation policy's step field.",
		codec:      &escalationStepOptionTableCodec{},
		fetch: func(ctx context.Context, client OnCallAPI) ([]EscalationStepOption, error) {
			return client.ListEscalationStepOptions(ctx)
		},
	})
}

// newWebhookTriggerCmds returns `webhooks list-triggers` and the older
// `webhooks triggers` noun group.
func newWebhookTriggerCmds(loader OnCallConfigLoader) []*cobra.Command {
	return newDiscoveryCmds(loader, discoveryCatalog[WebhookTriggerOption]{
		command:    "list-triggers",
		noun:       "triggers",
		groupShort: "Discover allowed webhook trigger types.",
		short:      "List allowed values for a webhook's trigger_type field.",
		codec:      &webhookTriggerOptionTableCodec{},
		fetch: func(ctx context.Context, client OnCallAPI) ([]WebhookTriggerOption, error) {
			return client.ListWebhookTriggerOptions(ctx)
		},
	})
}

// newWebhookPresetCmds returns `webhooks list-presets` and the older
// `webhooks presets` noun group.
func newWebhookPresetCmds(loader OnCallConfigLoader) []*cobra.Command {
	return newDiscoveryCmds(loader, discoveryCatalog[WebhookPreset]{
		command:    "list-presets",
		noun:       "presets",
		groupShort: "Discover webhook configuration presets.",
		short:      "List webhook preset IDs (e.g. grafana_assistant) and their allowed triggers.",
		codec:      &webhookPresetTableCodec{},
		fetch: func(ctx context.Context, client OnCallAPI) ([]WebhookPreset, error) {
			return client.ListWebhookPresets(ctx)
		},
	})
}

// newRouteFilterTypeCmds returns `routes list-filter-types` and the older
// `routes filter-types` noun group.
func newRouteFilterTypeCmds(loader OnCallConfigLoader) []*cobra.Command {
	return newDiscoveryCmds(loader, discoveryCatalog[RouteFilterType]{
		command:    "list-filter-types",
		noun:       "filter-types",
		groupShort: "Discover route filtering term types.",
		short:      "List allowed values for a route's filtering_term_type field.",
		codec:      &routeFilterTypeTableCodec{},
		fetch: func(ctx context.Context, client OnCallAPI) ([]RouteFilterType, error) {
			return client.ListRouteFilterTypes(ctx)
		},
	})
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
