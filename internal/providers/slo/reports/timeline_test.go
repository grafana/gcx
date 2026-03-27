package reports_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/providers/slo/definitions"
	"github.com/grafana/gcx/internal/providers/slo/reports"
)

// ---------------------------------------------------------------------------
// TestReportTimelineGraphCodec_Encode
// ---------------------------------------------------------------------------

func TestReportTimelineGraphCodecEncode(t *testing.T) {
	now := time.Now()

	makePoints := func(name, uuid string, n int, value, obj float64) []definitions.SLOTimeSeriesPoint {
		pts := make([]definitions.SLOTimeSeriesPoint, n)
		for i := range pts {
			pts[i] = definitions.SLOTimeSeriesPoint{
				SLOMetricPoint: definitions.SLOMetricPoint{
					UUID:      uuid,
					Name:      name,
					Value:     value,
					Objective: obj,
				},
				Time: now.Add(time.Duration(i) * time.Minute),
			}
		}
		return pts
	}

	rpts := []reports.Report{
		{
			UUID:     "rpt-1",
			Name:     "Weekly Platform Report",
			TimeSpan: "weeklySundayToSunday",
			ReportDefinition: reports.ReportDefinition{
				Slos: []reports.ReportSlo{
					{SloUUID: "uuid-1"},
					{SloUUID: "uuid-2"},
				},
			},
		},
	}

	sloIndex := map[string]definitions.Slo{
		"uuid-1": {UUID: "uuid-1", Name: "slo-alpha", Objectives: []definitions.Objective{{Value: 0.995}}},
		"uuid-2": {UUID: "uuid-2", Name: "slo-beta", Objectives: []definitions.Objective{{Value: 0.999}}},
	}

	allPoints := map[string][]definitions.SLOTimeSeriesPoint{
		"uuid-1": makePoints("slo-alpha", "uuid-1", 5, 0.997, 0.995),
		"uuid-2": makePoints("slo-beta", "uuid-2", 5, 0.998, 0.999),
	}

	tests := []struct {
		name        string
		payload     any
		wantErr     bool
		wantContent []string
	}{
		{
			name: "valid payload renders per-report chart",
			payload: reports.ReportTimelinePayload{
				Reports:  rpts,
				SLOIndex: sloIndex,
				Points:   allPoints,
				Start:    now.Add(-10 * time.Minute),
				End:      now,
			},
			wantErr: false,
		},
		{
			name: "empty points map prints no-data message",
			payload: reports.ReportTimelinePayload{
				Reports:  rpts,
				SLOIndex: sloIndex,
				Points:   map[string][]definitions.SLOTimeSeriesPoint{},
				Start:    now.Add(-10 * time.Minute),
				End:      now,
			},
			wantErr:     false,
			wantContent: []string{"No time-series data"},
		},
		{
			name:    "wrong type returns error",
			payload: "invalid",
			wantErr: true,
		},
	}

	codec := &reports.ReportTimelineGraphCodec{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := codec.Encode(&buf, tt.payload)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			output := buf.String()
			for _, want := range tt.wantContent {
				if !strings.Contains(output, want) {
					t.Errorf("expected %q in output:\n%s", want, output)
				}
			}
		})
	}
}

func TestReportTimelineGraphCodecDecode_NotSupported(t *testing.T) {
	codec := &reports.ReportTimelineGraphCodec{}
	err := codec.Decode(nil, nil)
	if err == nil {
		t.Error("expected error from Decode, got nil")
	}
}

// ---------------------------------------------------------------------------
// TestReportTimelineTableCodec_Encode
// ---------------------------------------------------------------------------

func TestReportTimelineTableCodecEncode(t *testing.T) {
	now := time.Now()

	makePoints := func(name, uuid string, n int, value, obj float64) []definitions.SLOTimeSeriesPoint {
		pts := make([]definitions.SLOTimeSeriesPoint, n)
		for i := range pts {
			pts[i] = definitions.SLOTimeSeriesPoint{
				SLOMetricPoint: definitions.SLOMetricPoint{
					UUID:      uuid,
					Name:      name,
					Value:     value,
					Objective: obj,
				},
				Time: now.Add(time.Duration(i) * time.Minute),
			}
		}
		return pts
	}

	rpts := []reports.Report{
		{
			UUID:     "rpt-1",
			Name:     "Monthly Report",
			TimeSpan: "calendarMonth",
			ReportDefinition: reports.ReportDefinition{
				Slos: []reports.ReportSlo{
					{SloUUID: "uuid-1"},
				},
			},
		},
	}

	sloIndex := map[string]definitions.Slo{
		"uuid-1": {UUID: "uuid-1", Name: "slo-alpha", Objectives: []definitions.Objective{{Value: 0.995}}},
	}

	tests := []struct {
		name        string
		payload     any
		wantErr     bool
		wantContent []string
	}{
		{
			name: "valid payload renders table with header and rows",
			payload: reports.ReportTimelinePayload{
				Reports:  rpts,
				SLOIndex: sloIndex,
				Points: map[string][]definitions.SLOTimeSeriesPoint{
					"uuid-1": makePoints("slo-alpha", "uuid-1", 3, 0.997, 0.995),
				},
				Start: now.Add(-3 * time.Minute),
				End:   now,
			},
			wantErr:     false,
			wantContent: []string{"REPORT", "NAME", "UUID", "TIMESTAMP", "SLI", "OBJECTIVE", "Monthly Report", "slo-alpha"},
		},
		{
			name: "empty payload renders header only without error",
			payload: reports.ReportTimelinePayload{
				Reports:  rpts,
				SLOIndex: sloIndex,
				Points:   map[string][]definitions.SLOTimeSeriesPoint{},
				Start:    now.Add(-time.Hour),
				End:      now,
			},
			wantErr:     false,
			wantContent: []string{"REPORT", "NAME"},
		},
		{
			name:    "wrong type returns error",
			payload: 42,
			wantErr: true,
		},
	}

	codec := &reports.ReportTimelineTableCodec{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := codec.Encode(&buf, tt.payload)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			output := buf.String()
			for _, want := range tt.wantContent {
				if !strings.Contains(output, want) {
					t.Errorf("expected %q in output:\n%s", want, output)
				}
			}
		})
	}
}

func TestReportTimelineTableCodecDecode_NotSupported(t *testing.T) {
	codec := &reports.ReportTimelineTableCodec{}
	err := codec.Decode(nil, nil)
	if err == nil {
		t.Error("expected error from Decode, got nil")
	}
}
