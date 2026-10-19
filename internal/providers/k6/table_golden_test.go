package k6_test

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/providers/k6"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/require"
)

func TestTablesGolden(t *testing.T) {
	tests := []struct {
		name      string
		codec     format.Codec
		populated any
		empty     any
	}{
		{"projects_table", k6.ProjectTable().Codec("table"), []k6.Project{{ID: 42, Name: "checkout", IsDefault: true, GrafanaFolderUID: "folder", Created: "2026-09-01T12:34:56Z", Updated: "2026-09-02T13:45:00Z"}, {ID: 7, Name: "minimal"}}, []k6.Project{}},
		{"projects_wide", k6.ProjectTable().Codec("wide"), []k6.Project{{ID: 42, Name: "checkout", IsDefault: true, GrafanaFolderUID: "folder", Created: "2026-09-01T12:34:56Z", Updated: "2026-09-02T13:45:00Z"}, {ID: 7, Name: "minimal"}}, []k6.Project{}},
		{"load_tests_table", k6.LoadTestTable().Codec("table"), []k6.LoadTest{{ID: 42, Name: "checkout", ProjectID: 7, Created: "2026-09-01T12:34:56Z", Updated: "2026-09-02T13:45:00Z"}, {ID: 7, Name: "minimal"}}, []k6.LoadTest{}},
		{"load_tests_wide", k6.LoadTestTable().Codec("wide"), []k6.LoadTest{{ID: 42, Name: "checkout", ProjectID: 7, Created: "2026-09-01T12:34:56Z", Updated: "2026-09-02T13:45:00Z"}, {ID: 7, Name: "minimal"}}, []k6.LoadTest{}},
		{"test_runs_table", k6.TestRunTable().Codec("table"), []k6.TestRunStatus{{ID: 44, TestID: 7, Status: "running", Created: "2026-09-03T12:34:56Z"}, {ID: 43, TestID: 7, Status: "completed", Result: new("failed"), Created: "2026-09-02T12:34:56Z", Ended: "2026-09-02T13:45:00Z"}, {ID: 42, TestID: 7, Status: "completed", Result: new("passed"), Created: "2026-09-01T12:34:56Z", Ended: "2026-09-01T13:45:00Z"}}, []k6.TestRunStatus{}},
		{"env_vars_table", k6.EnvVarTable().Codec("table"), []k6.EnvVar{{ID: 42, Name: "LONG_VALUE", Value: "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJ", Description: "a long value"}, {ID: 7, Name: "EMPTY"}}, []k6.EnvVar{}},
		{"schedules_table", k6.ScheduleTable().Codec("table"), []k6.Schedule{{ID: 42, LoadTestID: 7, Starts: "2026-09-01T12:34:56Z", NextRun: "2026-09-02T13:45:00Z", Deactivated: true, Cron: &k6.ScheduleCron{Schedule: "20 13 * * *", TimeZone: "Europe/London"}}, {ID: 43, LoadTestID: 8, Starts: "2026-09-01T12:34:56Z", RecurrenceRule: &k6.RecurrenceRule{Frequency: "WEEKLY", Interval: 2, ByDay: []string{"MO", "FR"}}}, {ID: 7}}, []k6.Schedule{}},
		{"load_zones_table", k6.LoadZoneTable().Codec("table"), []k6.LoadZone{{ID: 42, Name: "private", K6LoadZoneID: "zone-42"}, {ID: 7, Name: "minimal"}}, []k6.LoadZone{}},
		{"allowed_projects_table", k6.AllowedProjectTable().Codec("table"), []k6.AllowedProject{{ID: 42, Name: "checkout"}, {ID: 7}}, []k6.AllowedProject{}},
		{"allowed_load_zones_table", k6.AllowedLoadZoneTable().Codec("table"), []k6.AllowedLoadZone{{ID: 42, Name: "private"}, {ID: 7}}, []k6.AllowedLoadZone{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, data := range []struct {
				name string
				rows any
			}{{"populated", tt.populated}, {"empty", tt.empty}} {
				t.Run(data.name, func(t *testing.T) {
					var buf bytes.Buffer
					require.NoError(t, tt.codec.Encode(&buf, data.rows))
					testutils.Golden(t, tt.name+"_"+data.name, buf.String())
				})
			}
		})
	}
}
