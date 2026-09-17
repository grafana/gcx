package alert_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/providers/alert"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/require"
)

func TestAlertTableGolden(t *testing.T) {
	stamp := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		codec format.Codec
		rows  any
		empty any
	}{
		{"Groups_table", alert.GroupsTable().Codec("table"), []alert.RuleGroup{{Name: "production", FolderUID: "folder-1", Interval: 60, Rules: []alert.RuleStatus{{UID: "r1"}, {UID: "r2"}}}, {}}, []alert.RuleGroup{}},
		{"Rules_table", alert.RulesTable().Codec("table"), []alert.RuleStatus{{UID: "r1", Name: "High latency", State: "firing", Health: "ok", LastEvaluation: "2026-09-01T12:00:00Z", EvaluationTime: 0.1234, FolderUID: "folder-1", IsPaused: true}, {UID: "r2", LastEvaluation: "0001-01-01T00:00:00Z"}}, []alert.RuleStatus{}},
		{"Rules_wide", alert.RulesTable().Codec("wide"), []alert.RuleStatus{{UID: "r1", Name: "High latency", State: "firing", Health: "ok", LastEvaluation: "2026-09-01T12:00:00Z", EvaluationTime: 0.1234, FolderUID: "folder-1", IsPaused: true}, {UID: "r2", LastEvaluation: "0001-01-01T00:00:00Z"}}, []alert.RuleStatus{}},
		{"Instances_table", alert.InstancesTable().Codec("table"), []alert.AlertInstanceRecord{{RuleUID: "r1", RuleName: "High latency", GroupName: "production", FolderUID: "folder-1", State: "firing", ActiveAt: "2026-09-01T12:00:00Z", Value: 1.25, Labels: map[string]string{"service": "api", "env": "prod"}}, {}}, []alert.AlertInstanceRecord{}},
		{"Instances_wide", alert.InstancesTable().Codec("wide"), []alert.AlertInstanceRecord{{RuleUID: "r1", RuleName: "High latency", GroupName: "production", FolderUID: "folder-1", State: "firing", ActiveAt: "2026-09-01T12:00:00Z", Value: 1.25, Labels: map[string]string{"service": "api", "env": "prod"}}, {}}, []alert.AlertInstanceRecord{}},
		{"StateHistory_table", alert.StateHistoryTable().Codec("table"), []alert.StateTransition{{Time: stamp, RuleUID: "r1", RuleTitle: "High latency", Previous: "Normal", Current: "Alerting", Error: "query failed", Labels: map[string]string{"service": "api", "env": "prod"}}, {}}, []alert.StateTransition{}},
		{"StateHistory_wide", alert.StateHistoryTable().Codec("wide"), []alert.StateTransition{{Time: stamp, RuleUID: "r1", RuleTitle: "High latency", Previous: "Normal", Current: "Alerting", Error: "query failed", Labels: map[string]string{"service": "api", "env": "prod"}}, {}}, []alert.StateTransition{}},
		{"NotificationHistory_table", alert.NotificationHistoryTable().Codec("table"), []alert.NotificationEntry{{Timestamp: stamp, Receiver: "ops", Integration: "email", Status: "firing", Outcome: "error", AlertCount: 2, Duration: 1234567890, RuleUIDs: []string{"r1", "r2"}, GroupLabels: map[string]string{"service": "api", "env": "prod"}, UUID: "notification-1", Error: "delivery failed"}, {}}, []alert.NotificationEntry{}},
		{"NotificationHistory_wide", alert.NotificationHistoryTable().Codec("wide"), []alert.NotificationEntry{{Timestamp: stamp, Receiver: "ops", Integration: "email", Status: "firing", Outcome: "error", AlertCount: 2, Duration: 1234567890, RuleUIDs: []string{"r1", "r2"}, GroupLabels: map[string]string{"service": "api", "env": "prod"}, UUID: "notification-1", Error: "delivery failed"}, {}}, []alert.NotificationEntry{}},
		{"NotificationAlerts_table", alert.NotificationAlertsTable().Codec("table"), []alert.NotificationAlert{{Status: "resolved", StartsAt: stamp, EndsAt: stamp.Add(time.Hour), Labels: map[string]string{"service": "api", "env": "prod"}}, {}}, []alert.NotificationAlert{}},
		{"MuteTimings_table", alert.MuteTimingsTable().Codec("table"), []alert.MuteTiming{{Name: "weekend", TimeIntervals: []alert.TimeInterval{{Weekdays: []string{"saturday", "sunday"}, Times: []alert.TimeRangeHHMM{{Start: "09:00", End: "17:00"}}}}}, {}}, []alert.MuteTiming{}},
		{"ContactPoints_table", alert.ContactPointsTable().Codec("table"), []alert.ContactPoint{{UID: "cp-1", Name: "ops", Type: "email", Provenance: "api"}, {}}, []alert.ContactPoint{}},
		{"Templates_table", alert.TemplatesTable().Codec("table"), []alert.NotificationTemplate{{Name: "message", Provenance: "api", Template: "Hello \u4e16\u754c"}, {}}, []alert.NotificationTemplate{}},
		{"RulerNamespaces_table", alert.RulerNamespacesTable().Codec("table"), []alert.RulerNamespaceView{{Namespace: "production", Groups: 2, Rules: 3}, {}}, []alert.RulerNamespaceView{}},
		{"RulerGroups_table", alert.RulerGroupsTable().Codec("table"), []alert.RulerGroupView{{Namespace: "production", Group: "latency", Interval: "1m", Rules: 2}, {}}, []alert.RulerGroupView{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			require.NoError(t, tt.codec.Encode(&buf, tt.rows))
			testutils.Golden(t, tt.name, buf.String())
			buf.Reset()
			require.NoError(t, tt.codec.Encode(&buf, tt.empty))
			testutils.Golden(t, tt.name+"_empty", buf.String())
		})
	}
}
