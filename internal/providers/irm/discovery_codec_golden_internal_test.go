package irm

import (
	"bytes"
	"testing"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/require"
)

// Each fixture pairs a fully populated row with one leaving the optional
// fields empty, so the goldens pin the blank renderings too.

func goldenEscalationStepOptions() []EscalationStepOption {
	return []EscalationStepOption{
		{Value: 19, CreateDisplayName: "Declare Incident", DisplayName: "declare incident", SlackIntegrationRequired: true},
		{Value: 0},
	}
}

func goldenWebhookTriggerOptions() []WebhookTriggerOption {
	return []WebhookTriggerOption{
		{Value: 12, DisplayName: "Incident Changed"},
		{Value: 0},
	}
}

func goldenWebhookPresets() []WebhookPreset {
	return []WebhookPreset{
		{
			ID:          "preset-1",
			Name:        "ServiceNow",
			Description: "Create a ServiceNow ticket",
			TriggerTypes: []WebhookPresetTriggerType{
				{Value: "alert group created"},
				{Value: "resolved"},
			},
		},
		{ID: "preset-2", Name: "bare"},
	}
}

func goldenRouteFilterTypes() []RouteFilterType {
	return []RouteFilterType{
		{Value: 0, DisplayName: "Regex"},
		{Value: 1},
	}
}

func TestDiscoveryTableGolden(t *testing.T) {
	var esc bytes.Buffer
	require.NoError(t, escalationStepOptionTable().Codec(cmdio.FormatTable).Encode(&esc, goldenEscalationStepOptions()))
	testutils.Golden(t, "discovery_escalation_steps", esc.String())

	var trig bytes.Buffer
	require.NoError(t, webhookTriggerOptionTable().Codec(cmdio.FormatTable).Encode(&trig, goldenWebhookTriggerOptions()))
	testutils.Golden(t, "discovery_webhook_triggers", trig.String())

	var preset bytes.Buffer
	require.NoError(t, webhookPresetTable().Codec(cmdio.FormatTable).Encode(&preset, goldenWebhookPresets()))
	testutils.Golden(t, "discovery_webhook_presets", preset.String())

	var filter bytes.Buffer
	require.NoError(t, routeFilterTypeTable().Codec(cmdio.FormatTable).Encode(&filter, goldenRouteFilterTypes()))
	testutils.Golden(t, "discovery_route_filter_types", filter.String())

	var empty bytes.Buffer
	require.NoError(t, routeFilterTypeTable().Codec(cmdio.FormatTable).Encode(&empty, []RouteFilterType{}))
	testutils.Golden(t, "discovery_route_filter_types_empty", empty.String())
}
