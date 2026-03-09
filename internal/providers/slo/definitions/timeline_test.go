package definitions_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/grafana/grafanactl/internal/providers/slo/definitions"
	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// TestAutoStep
// ---------------------------------------------------------------------------

func TestAutoStep(t *testing.T) {
	tests := []struct {
		name        string
		rangeSize   time.Duration
		wantAtLeast time.Duration
		wantAtMost  time.Duration
	}{
		{
			name:        "7d range targets ~200 points with minute truncation",
			rangeSize:   7 * 24 * time.Hour,
			wantAtLeast: 49 * time.Minute,
			wantAtMost:  51 * time.Minute,
		},
		{
			name:        "1h range clamps to minimum 1m step",
			rangeSize:   time.Hour,
			wantAtLeast: time.Minute,
			wantAtMost:  time.Minute,
		},
		{
			name:        "24h range is around 7m",
			rangeSize:   24 * time.Hour,
			wantAtLeast: 6 * time.Minute,
			wantAtMost:  8 * time.Minute,
		},
		{
			name:        "30d range produces correct step",
			rangeSize:   30 * 24 * time.Hour,
			wantAtLeast: 3*time.Hour + 30*time.Minute,
			wantAtMost:  3*time.Hour + 40*time.Minute,
		},
	}

	now := time.Now()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start := now.Add(-tt.rangeSize)
			end := now
			got := definitions.AutoStep(start, end)

			if got < tt.wantAtLeast || got > tt.wantAtMost {
				t.Errorf("AutoStep(%v) = %v, want [%v, %v]",
					tt.rangeSize, got, tt.wantAtLeast, tt.wantAtMost)
			}

			// Result must always be a whole-minute multiple.
			if got%time.Minute != 0 {
				t.Errorf("AutoStep(%v) = %v is not a whole-minute multiple", tt.rangeSize, got)
			}
		})
	}
}

func TestAutoStep_MinimumClamp(t *testing.T) {
	// Even a tiny range (10 seconds) should return at least 1m.
	now := time.Now()
	start := now.Add(-10 * time.Second)
	got := definitions.AutoStep(start, now)
	if got < time.Minute {
		t.Errorf("AutoStep should be at least 1m, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// TestParseMatrixValues — unit-tests the matrix value parsing helper
// ---------------------------------------------------------------------------

func TestParseMatrixValues(t *testing.T) {
	now := time.Now()
	meta := definitions.SLOMetricPoint{
		UUID:      "test-uuid",
		Name:      "test-slo",
		Objective: 0.995,
	}

	tests := []struct {
		name      string
		values    [][]any
		wantCount int
		wantFirst float64
	}{
		{
			name: "valid string values are parsed",
			values: [][]any{
				{float64(1700000000), "0.9972"},
				{float64(1700000060), "0.9980"},
			},
			wantCount: 2,
			wantFirst: 0.9972,
		},
		{
			name: "NaN value is skipped",
			values: [][]any{
				{float64(1700000000), "NaN"},
				{float64(1700000060), "0.9972"},
			},
			wantCount: 1,
			wantFirst: 0.9972,
		},
		{
			name:      "empty slice yields no points",
			values:    [][]any{},
			wantCount: 0,
		},
		{
			name: "malformed element (too short) is skipped",
			values: [][]any{
				{float64(1700000000)}, // only 1 element — no value
				{float64(1700000060), "0.9972"},
			},
			wantCount: 1,
			wantFirst: 0.9972,
		},
		{
			name: "Inf value is skipped",
			values: [][]any{
				{float64(1700000000), "+Inf"},
				{float64(1700000060), "0.9972"},
			},
			wantCount: 1,
			wantFirst: 0.9972,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pts := definitions.ParseMatrixValues(tt.values, meta, now)

			if len(pts) != tt.wantCount {
				t.Errorf("ParseMatrixValues() count = %d, want %d", len(pts), tt.wantCount)
				return
			}

			if tt.wantCount == 0 {
				return
			}

			if pts[0].Value != tt.wantFirst {
				t.Errorf("first point value = %v, want %v", pts[0].Value, tt.wantFirst)
			}

			// Every point must carry the correct metadata.
			for i, pt := range pts {
				if pt.UUID != meta.UUID {
					t.Errorf("pts[%d].UUID = %q, want %q", i, pt.UUID, meta.UUID)
				}
				if pt.Name != meta.Name {
					t.Errorf("pts[%d].Name = %q, want %q", i, pt.Name, meta.Name)
				}
				if pt.Objective != meta.Objective {
					t.Errorf("pts[%d].Objective = %v, want %v", i, pt.Objective, meta.Objective)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestTimelineGraphCodec_Encode
// ---------------------------------------------------------------------------

func TestTimelineGraphCodecEncode(t *testing.T) {
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

	slos := []definitions.Slo{
		{UUID: "uuid-1", Name: "slo-alpha", Objectives: []definitions.Objective{{Value: 0.995}}},
		{UUID: "uuid-2", Name: "slo-beta", Objectives: []definitions.Objective{{Value: 0.999}}},
	}

	tests := []struct {
		name        string
		payload     any
		wantErr     bool
		wantContent []string
	}{
		{
			name: "valid payload with two SLOs renders without error",
			payload: definitions.SLITrendPayload{
				SLOs: slos,
				Points: map[string][]definitions.SLOTimeSeriesPoint{
					"uuid-1": makePoints("slo-alpha", "uuid-1", 5, 0.997, 0.995),
					"uuid-2": makePoints("slo-beta", "uuid-2", 5, 0.998, 0.999),
				},
				Start: now.Add(-10 * time.Minute),
				End:   now,
			},
			wantErr: false,
		},
		{
			name: "empty Points map prints no-data message",
			payload: definitions.SLITrendPayload{
				SLOs:   slos,
				Points: map[string][]definitions.SLOTimeSeriesPoint{},
				Start:  now.Add(-10 * time.Minute),
				End:    now,
			},
			wantErr:     false,
			wantContent: []string{"No time-series data"},
		},
		{
			name:    "wrong type returns error",
			payload: "not a payload",
			wantErr: true,
		},
	}

	codec := &definitions.TimelineGraphCodec{}

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

func TestTimelineGraphCodecDecode_NotSupported(t *testing.T) {
	codec := &definitions.TimelineGraphCodec{}
	err := codec.Decode(nil, nil)
	if err == nil {
		t.Error("expected error from Decode, got nil")
	}
}

// ---------------------------------------------------------------------------
// TestTimelineTableCodec_Encode
// ---------------------------------------------------------------------------

func TestTimelineTableCodecEncode(t *testing.T) {
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

	slos := []definitions.Slo{
		{UUID: "uuid-1", Name: "slo-alpha", Objectives: []definitions.Objective{{Value: 0.995}}},
	}

	tests := []struct {
		name        string
		payload     any
		wantErr     bool
		wantContent []string
	}{
		{
			name: "valid payload renders table with header and rows",
			payload: definitions.SLITrendPayload{
				SLOs: slos,
				Points: map[string][]definitions.SLOTimeSeriesPoint{
					"uuid-1": makePoints("slo-alpha", "uuid-1", 3, 0.997, 0.995),
				},
				Start: now.Add(-3 * time.Minute),
				End:   now,
			},
			wantErr:     false,
			wantContent: []string{"NAME", "UUID", "TIMESTAMP", "SLI", "OBJECTIVE", "slo-alpha", "uuid-1"},
		},
		{
			name: "empty payload renders header only without error",
			payload: definitions.SLITrendPayload{
				SLOs:   slos,
				Points: map[string][]definitions.SLOTimeSeriesPoint{},
				Start:  now.Add(-time.Hour),
				End:    now,
			},
			wantErr:     false,
			wantContent: []string{"NAME", "UUID"},
		},
		{
			name:    "wrong type returns error",
			payload: 42,
			wantErr: true,
		},
	}

	codec := &definitions.TimelineTableCodec{}

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

func TestTimelineTableCodecDecode_NotSupported(t *testing.T) {
	codec := &definitions.TimelineTableCodec{}
	err := codec.Decode(nil, nil)
	if err == nil {
		t.Error("expected error from Decode, got nil")
	}
}

// ---------------------------------------------------------------------------
// TestValidateTimelineFlags — unit-tests the flag mutual exclusivity check
// ---------------------------------------------------------------------------

func TestValidateTimelineFlags(t *testing.T) {
	tests := []struct {
		name    string
		flags   map[string]string // flag name → value
		wantErr bool
	}{
		{
			name:    "no flags set is valid",
			flags:   map[string]string{},
			wantErr: false,
		},
		{
			name:    "only --from and --to is valid",
			flags:   map[string]string{"from": "now-24h", "to": "now"},
			wantErr: false,
		},
		{
			name:    "only --window is valid",
			flags:   map[string]string{"window": "24h"},
			wantErr: false,
		},
		{
			name:    "--window with --from is mutually exclusive",
			flags:   map[string]string{"window": "24h", "from": "now-1h"},
			wantErr: true,
		},
		{
			name:    "--window with --to is mutually exclusive",
			flags:   map[string]string{"window": "24h", "to": "now"},
			wantErr: true,
		},
		{
			name:    "--window with deprecated --start is mutually exclusive",
			flags:   map[string]string{"window": "24h", "start": "now-1h"},
			wantErr: true,
		},
		{
			name:    "--window with deprecated --end is mutually exclusive",
			flags:   map[string]string{"window": "24h", "end": "now"},
			wantErr: true,
		},
		{
			name:    "only deprecated --start/--end is valid",
			flags:   map[string]string{"start": "now-24h", "end": "now"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &cobra.Command{Use: "test"}
			var from, to, window, start, end string
			cmd.Flags().StringVar(&from, "from", "now-7d", "")
			cmd.Flags().StringVar(&to, "to", "now", "")
			cmd.Flags().StringVar(&window, "window", "", "")
			cmd.Flags().StringVar(&start, "start", "now-7d", "")
			cmd.Flags().StringVar(&end, "end", "now", "")

			// Simulate flag setting.
			for k, v := range tt.flags {
				if err := cmd.Flags().Set(k, v); err != nil {
					t.Fatalf("failed to set flag --%s: %v", k, err)
				}
			}

			err := definitions.ValidateTimelineFlags(cmd)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestParseTimelineTime — tests the time parsing used by timeline commands
// ---------------------------------------------------------------------------

func TestParseTimelineTime(t *testing.T) {
	now := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		input   string
		want    time.Time
		wantErr bool
	}{
		{name: "now", input: "now", want: now},
		{name: "now-7d", input: "now-7d", want: now.Add(-7 * 24 * time.Hour)},
		{name: "now-24h", input: "now-24h", want: now.Add(-24 * time.Hour)},
		{name: "empty defaults to now", input: "", want: now},
		{name: "RFC3339", input: "2026-03-01T00:00:00Z", want: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)},
		{name: "invalid", input: "garbage", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := definitions.ParseTimelineTime(tt.input, now)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !got.Equal(tt.want) {
				t.Errorf("ParseTimelineTime(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
