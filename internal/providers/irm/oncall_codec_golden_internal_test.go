package irm

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// obj builds one OnCall list item. The codecs read everything through
// specStr/specInt/specBool, so a spec map plus a name is the whole shape.
func obj(name string, spec map[string]any) unstructured.Unstructured {
	u := unstructured.Unstructured{Object: map[string]any{"spec": spec}}
	u.SetName(name)

	return u
}

// longText is over every truncation limit these tables apply, so the goldens
// pin the cut as well as the columns.
const longText = "Escalate to the on-call engineer and page the incident commander immediately"

// goldenOnCallRows is one populated row and one leaving every optional field
// unset, so the goldens pin the dash placeholders too.
func goldenOnCallRows(spec map[string]any) []unstructured.Unstructured {
	return []unstructured.Unstructured{obj("row-1", spec), obj("row-2", map[string]any{})}
}

func encodeGolden(t *testing.T, name string, codec format.Codec, rows []unstructured.Unstructured) {
	t.Helper()

	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, rows))
	testutils.Golden(t, name, buf.String())
}

func TestOnCallTableGolden(t *testing.T) {
	cases := []struct {
		name  string
		spec  map[string]any
		table func(wide bool) format.Codec
		wide  bool
	}{
		{
			name: "integrations",
			spec: map[string]any{"verbal_name": longText, "integration": "grafana", "team": "sre", "integration_url": "https://example.invalid/hook"},
			table: func(wide bool) format.Codec {
				return integrationTable().Codec(oncallFormat(wide))
			},
			wide: true,
		},
		{
			name:  "escalation_chains",
			spec:  map[string]any{"name": "primary", "team": "sre"},
			table: func(bool) format.Codec { return escalationChainTable().Codec("table") },
		},
		{
			name: "escalation_policies",
			spec: map[string]any{"escalation_chain": "primary", "step": "notify_persons", "wait_delay": "60", "important": true, "notify_schedule": "weekday"},
			table: func(wide bool) format.Codec {
				return escalationPolicyTable().Codec(oncallFormat(wide))
			},
			wide: true,
		},
		{
			name: "schedules",
			spec: map[string]any{"name": "weekday", "type": "calendar", "time_zone": "Europe/London", "team": "sre"},
			table: func(wide bool) format.Codec {
				return scheduleTable().Codec(oncallFormat(wide))
			},
			wide: true,
		},
		{
			name: "shifts",
			spec: map[string]any{"name": "morning", "type": "rolling_users", "shift_start": "2026-03-14T09:00:00Z", "shift_end": "2026-03-14T17:00:00Z", "frequency": "daily", "interval": int64(2)},
			table: func(wide bool) format.Codec {
				return shiftTable().Codec(oncallFormat(wide))
			},
			wide: true,
		},
		{
			name: "routes",
			spec: map[string]any{"alert_receive_channel": "grafana", "escalation_chain": "primary", "filtering_term_type": "regex", "filtering_term": longText, "is_default": true},
			table: func(wide bool) format.Codec {
				return routeTable().Codec(oncallFormat(wide))
			},
			wide: true,
		},
		{
			name: "webhooks",
			spec: map[string]any{"name": "servicenow", "url": "https://example.invalid/wh", "http_method": "POST", "trigger_type": "resolved", "is_webhook_enabled": true},
			table: func(wide bool) format.Codec {
				return webhookTable().Codec(oncallFormat(wide))
			},
			wide: true,
		},
		{
			name: "users",
			spec: map[string]any{"username": "someone", "name": "Some One", "email": "someone@example.invalid", "role": "admin", "timezone": "Europe/London"},
			table: func(wide bool) format.Codec {
				return userTable().Codec(oncallFormat(wide))
			},
			wide: true,
		},
		{
			name:  "teams",
			spec:  map[string]any{"name": "sre", "email": "sre@example.invalid"},
			table: func(bool) format.Codec { return teamTable().Codec("table") },
		},
		{
			name:  "user_groups",
			spec:  map[string]any{"name": "oncall", "handle": "@oncall"},
			table: func(bool) format.Codec { return userGroupTable().Codec("table") },
		},
		{
			name:  "slack_channels",
			spec:  map[string]any{"display_name": "#alerts", "slack_id": "C123"},
			table: func(bool) format.Codec { return slackChannelTable().Codec("table") },
		},
		{
			name:  "organizations",
			spec:  map[string]any{"name": "grafana", "stack_slug": "example"},
			table: func(bool) format.Codec { return organizationTable().Codec("table") },
		},
		{
			name: "resolution_notes",
			spec: map[string]any{"alert_group": "ag-1", "source": "web", "created_at": "2026-03-14T09:26:00.000Z", "text": longText},
			table: func(wide bool) format.Codec {
				return resolutionNoteTable().Codec(oncallFormat(wide))
			},
			wide: true,
		},
		{
			name: "shift_swaps",
			spec: map[string]any{"schedule": "weekday", "status": "open", "swap_start": "2026-03-14T09:00:00.000Z", "swap_end": "2026-03-15T09:00:00.000Z", "beneficiary": "someone", "benefactor": "someone-else", "created_at": "2026-03-13T09:00:00.000Z"},
			table: func(wide bool) format.Codec {
				return shiftSwapTable().Codec(oncallFormat(wide))
			},
			wide: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows := goldenOnCallRows(tc.spec)

			encodeGolden(t, "oncall_"+tc.name+"_table", tc.table(false), rows)
			if tc.wide {
				encodeGolden(t, "oncall_"+tc.name+"_wide", tc.table(true), rows)
			}
		})
	}
}

func TestOnCallTableGoldenEmpty(t *testing.T) {
	encodeGolden(t, "oncall_teams_table_empty", teamTable().Codec("table"), []unstructured.Unstructured{})
}

// oncallFormat maps the wide flag these fixtures use to the registered format
// name the codec is built for.
func oncallFormat(wide bool) string {
	if wide {
		return "wide"
	}
	return "table"
}
