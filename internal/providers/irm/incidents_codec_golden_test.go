package irm_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/providers/irm"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/require"
)

// longTitle is over the 50-character limit the narrow incident table applies,
// so the goldens pin the truncation as well as the columns.
const longTitle = "Checkout latency regression affecting EU customers since the deploy"

func goldenIncidents() []irm.Incident {
	created := irm.FlexTime(time.Date(2026, time.March, 14, 9, 26, 0, 0, time.UTC))

	return []irm.Incident{
		{
			IncidentID:   "inc-1",
			Title:        longTitle,
			Status:       "active",
			Severity:     "Critical",
			IncidentType: "outage",
			CreatedTime:  created,
		},
		{IncidentID: "inc-2", Title: "bare", Status: "resolved"},
	}
}

func goldenActivityItems() []irm.ActivityItem {
	return []irm.ActivityItem{
		{
			ActivityItemID: "act-1",
			ActivityKind:   "userNote",
			User:           irm.ActivityUser{Name: "someone"},
			EventTime:      "2026-03-14T09:26:00.000Z",
			Body:           "first line\nsecond line",
		},
		{ActivityItemID: "act-2", ActivityKind: "incidentCreated", CreatedTime: "2026-03-14T09:00:00.000Z"},
	}
}

func goldenSeverities() []irm.Severity {
	return []irm.Severity{
		{SeverityID: "sev-1", Level: 1, DisplayLabel: "Critical", Color: "red"},
		{SeverityID: "sev-2", Level: 4, DisplayLabel: "Minor"},
	}
}

func goldenIncidentContexts() []irm.IncidentContext {
	alertGroup := "ag-99"

	return []irm.IncidentContext{
		{
			ContextID:    "ctx-1",
			Type:         "alertGroup",
			Status:       "firing",
			AlertGroupID: &alertGroup,
			Title:        longTitle,
			CreatedTime:  "2026-03-14T09:26:00.000Z",
		},
		{ContextID: "ctx-2"},
	}
}

func TestIncidentTableGolden(t *testing.T) {
	for _, name := range []string{"table", "wide"} {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			require.NoError(t, irm.IncidentTable().Codec(name).Encode(&buf, goldenIncidents()))

			testutils.Golden(t, "incidents_"+name, buf.String())
		})
	}
}

func TestIncidentContextTableGolden(t *testing.T) {
	for _, name := range []string{"table", "wide"} {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			require.NoError(t, irm.IncidentContextTable().Codec(name).Encode(&buf, goldenIncidentContexts()))

			testutils.Golden(t, "incident_contexts_"+name, buf.String())
		})
	}
}

func TestActivityTableGolden(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, irm.ActivityTable().Codec("table").Encode(&buf, goldenActivityItems()))

	testutils.Golden(t, "incident_activity_table", buf.String())
}

func TestSeverityTableGolden(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, irm.SeverityTable().Codec("table").Encode(&buf, goldenSeverities()))

	testutils.Golden(t, "incident_severities_table", buf.String())
}

func TestIncidentTableGoldenEmpty(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, irm.IncidentTable().Codec("table").Encode(&buf, []irm.Incident{}))

	testutils.Golden(t, "incidents_table_empty", buf.String())
}
