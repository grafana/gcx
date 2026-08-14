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
	// parent is the resource noun that owns the catalog, for example
	// "escalation-policies".
	parent string
	// command is the canonical compound command, for example
	// "list-step-types".
	command string
	// noun is the older noun group, for example "steps".
	noun string
	// groupShort is the one-line help of the older noun group.
	groupShort string
	// short is the one-line help of the catalog command.
	short string
	// long is the multi-sentence help of the catalog command.
	long string
	// example holds the examples of the catalog command.
	example string

	codec format.Codec
	fetch func(ctx context.Context, client OnCallAPI) ([]T, error)
}

// legacyShort gives the older `<noun> list` child a one-line help that names
// the current spelling, so a person who reads the parent help sees which of
// the two invocations to use.
func (c discoveryCatalog[T]) legacyShort() string {
	return strings.TrimSuffix(c.short, ".") +
		" (older spelling; use `" + c.parent + " " + c.command + "`)."
}

// newListCmd builds a command that fetches the catalog and emits it through
// the codec system. Each call makes its own opts and binds them to the new
// command's local flag set, so the two commands of one catalog never share
// flag state.
func (c discoveryCatalog[T]) newListCmd(loader OnCallConfigLoader, use, short string) *cobra.Command {
	opts := &discoveryListOpts{}
	cmd := &cobra.Command{
		Use:     use,
		Short:   short,
		Long:    c.long,
		Example: c.example,
		Args:    cobra.NoArgs,
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
	group.AddCommand(c.newListCmd(loader, "list", c.legacyShort()))

	return []*cobra.Command{
		c.newListCmd(loader, c.command, c.short),
		group,
	}
}

// newEscalationStepCmds returns `escalation-policies list-step-types` and the
// older `escalation-policies steps` noun group.
func newEscalationStepCmds(loader OnCallConfigLoader) []*cobra.Command {
	return newDiscoveryCmds(loader, discoveryCatalog[EscalationStepOption]{
		parent:     "escalation-policies",
		command:    "list-step-types",
		noun:       "steps",
		groupShort: "Discover allowed escalation policy step types.",
		short:      "List allowed values for an escalation policy's step field.",
		long: "List the step types that an escalation policy accepts. " +
			"The command reads the catalog from the Incident Response and Management backend, " +
			"so the values match your stack. " +
			"Put the numeric value in the step field of an escalation policy manifest.",
		example: `  # List the step types that an escalation policy accepts
  gcx irm oncall escalation-policies list-step-types

  # Read the numeric value of one step type
  gcx irm oncall escalation-policies list-step-types -o json | jq -r '.[] | select(.display_name == "<display-name>") | .value'

  # Put that value in the step field of policy.yaml, then create the policy
  gcx irm oncall escalation-policies create -f policy.yaml`,
		codec: &escalationStepOptionTableCodec{},
		fetch: func(ctx context.Context, client OnCallAPI) ([]EscalationStepOption, error) {
			return client.ListEscalationStepOptions(ctx)
		},
	})
}

// newWebhookTriggerCmds returns `webhooks list-triggers` and the older
// `webhooks triggers` noun group.
func newWebhookTriggerCmds(loader OnCallConfigLoader) []*cobra.Command {
	return newDiscoveryCmds(loader, discoveryCatalog[WebhookTriggerOption]{
		parent:     "webhooks",
		command:    "list-triggers",
		noun:       "triggers",
		groupShort: "Discover allowed webhook trigger types.",
		short:      "List allowed values for a webhook's trigger_type field.",
		long: "List the trigger types that an outgoing webhook accepts. " +
			"The command reads the catalog from the Incident Response and Management backend, " +
			"so the values match your stack. " +
			"Put the numeric value in the trigger_type field of a webhook manifest.",
		example: `  # List the trigger types that a webhook accepts
  gcx irm oncall webhooks list-triggers

  # Read the numeric value of one trigger type
  gcx irm oncall webhooks list-triggers -o json | jq -r '.[] | select(.display_name == "<display-name>") | .value'

  # Put that value in the trigger_type field of webhook.yaml, then create the webhook
  gcx irm oncall webhooks create -f webhook.yaml`,
		codec: &webhookTriggerOptionTableCodec{},
		fetch: func(ctx context.Context, client OnCallAPI) ([]WebhookTriggerOption, error) {
			return client.ListWebhookTriggerOptions(ctx)
		},
	})
}

// newWebhookPresetCmds returns `webhooks list-presets` and the older
// `webhooks presets` noun group.
func newWebhookPresetCmds(loader OnCallConfigLoader) []*cobra.Command {
	return newDiscoveryCmds(loader, discoveryCatalog[WebhookPreset]{
		parent:     "webhooks",
		command:    "list-presets",
		noun:       "presets",
		groupShort: "Discover webhook configuration presets.",
		short:      "List webhook preset IDs (e.g. grafana_assistant) and their allowed triggers.",
		long: "List the presets that an outgoing webhook accepts. " +
			"A preset fills a group of webhook fields, and it limits the trigger types of the webhook. " +
			"Put the preset ID in the preset field of a webhook manifest.",
		example: `  # List the webhook presets
  gcx irm oncall webhooks list-presets

  # Read the trigger types that one preset allows
  gcx irm oncall webhooks list-presets -o json | jq -r '.[] | select(.id == "grafana_assistant") | .trigger_types'

  # Put the preset ID in the preset field of webhook.yaml, then create the webhook
  gcx irm oncall webhooks create -f webhook.yaml`,
		codec: &webhookPresetTableCodec{},
		fetch: func(ctx context.Context, client OnCallAPI) ([]WebhookPreset, error) {
			return client.ListWebhookPresets(ctx)
		},
	})
}

// newRouteFilterTypeCmds returns `routes list-filter-types` and the older
// `routes filter-types` noun group.
func newRouteFilterTypeCmds(loader OnCallConfigLoader) []*cobra.Command {
	return newDiscoveryCmds(loader, discoveryCatalog[RouteFilterType]{
		parent:     "routes",
		command:    "list-filter-types",
		noun:       "filter-types",
		groupShort: "Discover route filtering term types.",
		short:      "List allowed values for a route's filtering_term_type field.",
		long: "List the filter types that a route accepts. " +
			"The command reads the catalog from the Incident Response and Management backend, " +
			"so the values match your stack. " +
			"Put the numeric value in the filtering_term_type field of a route manifest.",
		example: `  # List the filter types that a route accepts
  gcx irm oncall routes list-filter-types

  # Read the numeric value of one filter type
  gcx irm oncall routes list-filter-types -o json | jq -r '.[] | select(.display_name == "<display-name>") | .value'

  # Put that value in the filtering_term_type field of route.yaml, then create the route
  gcx irm oncall routes create -f route.yaml`,
		codec: &routeFilterTypeTableCodec{},
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
