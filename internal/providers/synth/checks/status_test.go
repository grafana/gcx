package checks_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/providers/synth/checks"
)

// ---------------------------------------------------------------------------
// PromQL query builders
// ---------------------------------------------------------------------------

func TestBuildSuccessRateQuery(t *testing.T) {
	tests := []struct {
		name     string
		job      string
		instance string
		want     string
	}{
		{
			name:     "basic success rate query",
			job:      "my-check",
			instance: "https://example.com",
			want:     `avg by (job, instance) (avg_over_time(probe_success{job="my-check",instance="https://example.com"}[5m]))`,
		},
		{
			name:     "job with special chars",
			job:      "check-http-prod",
			instance: "https://api.example.com/health",
			want:     `avg by (job, instance) (avg_over_time(probe_success{job="check-http-prod",instance="https://api.example.com/health"}[5m]))`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := checks.BuildSuccessRateQuery(tt.job, tt.instance)
			if err != nil {
				t.Fatalf("BuildSuccessRateQuery() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("BuildSuccessRateQuery() =\n  %s\nwant\n  %s", got, tt.want)
			}
		})
	}
}

func TestBuildProbeCountQuery(t *testing.T) {
	tests := []struct {
		name     string
		job      string
		instance string
		want     string
	}{
		{
			name:     "basic probe count query",
			job:      "my-check",
			instance: "https://example.com",
			want:     `count by (job, instance) (probe_success{job="my-check",instance="https://example.com"})`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := checks.BuildProbeCountQuery(tt.job, tt.instance)
			if err != nil {
				t.Fatalf("BuildProbeCountQuery() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("BuildProbeCountQuery() =\n  %s\nwant\n  %s", got, tt.want)
			}
		})
	}
}

func TestBuildTimelineQuery(t *testing.T) {
	tests := []struct {
		name     string
		job      string
		instance string
		want     string
	}{
		{
			name:     "basic timeline query",
			job:      "my-check",
			instance: "https://example.com",
			want:     `probe_success{job="my-check",instance="https://example.com"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := checks.BuildTimelineQuery(tt.job, tt.instance)
			if err != nil {
				t.Fatalf("BuildTimelineQuery() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("BuildTimelineQuery() =\n  %s\nwant\n  %s", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Status table codec
// ---------------------------------------------------------------------------

func TestStatusTableCodec_Encode(t *testing.T) {
	results := []checks.CheckStatusResult{
		{
			ID:          1,
			Job:         "http-check",
			Target:      "https://example.com",
			Type:        "http",
			Success:     new(0.9972),
			ProbesUp:    3,
			ProbesTotal: 3,
			Status:      "OK",
		},
		{
			ID:          2,
			Job:         "ping-check",
			Target:      "10.0.0.1",
			Type:        "ping",
			Success:     new(0.0),
			ProbesUp:    0,
			ProbesTotal: 2,
			Status:      "FAILING",
		},
		{
			ID:          3,
			Job:         "new-check",
			Target:      "https://new.example.com",
			Type:        "http",
			Success:     nil,
			ProbesUp:    0,
			ProbesTotal: 1,
			Status:      "NODATA",
		},
	}

	t.Run("table output", func(t *testing.T) {
		codec := &checks.StatusTableCodec{}
		var buf bytes.Buffer
		err := codec.Encode(&buf, results)
		if err != nil {
			t.Fatalf("Encode() error = %v", err)
		}

		output := buf.String()

		// Verify header columns present in default table.
		for _, col := range []string{"NAME", "JOB", "TARGET", "SUCCESS", "STATUS"} {
			if !strings.Contains(output, col) {
				t.Errorf("missing header column %q in:\n%s", col, output)
			}
		}

		// Verify wide-only columns are absent from default table.
		for _, col := range []string{"TYPE", "PROBES_UP", "PROBES_TOTAL", "PROBES"} {
			if strings.Contains(output, col) {
				t.Errorf("default table should not have column %q:\n%s", col, output)
			}
		}

		// Verify data rows.
		if !strings.Contains(output, "http-check") {
			t.Errorf("missing http-check in:\n%s", output)
		}
		if !strings.Contains(output, "99.72%") {
			t.Errorf("missing 99.72%% in:\n%s", output)
		}
		if !strings.Contains(output, "OK") {
			t.Errorf("missing OK status in:\n%s", output)
		}
		if !strings.Contains(output, "FAILING") {
			t.Errorf("missing FAILING status in:\n%s", output)
		}
		if !strings.Contains(output, "NODATA") {
			t.Errorf("missing NODATA status in:\n%s", output)
		}
		if !strings.Contains(output, "--") {
			t.Errorf("missing -- for nil success in:\n%s", output)
		}
	})

	t.Run("wide output", func(t *testing.T) {
		wideResults := []checks.CheckStatusResult{
			{
				ID:          1,
				Job:         "http-check",
				Target:      "https://example.com",
				Type:        "http",
				Success:     new(0.9972),
				ProbesUp:    2,
				ProbesTotal: 2,
				ProbeNames:  []string{"Oregon", "Paris (offline)"},
				Status:      "OK",
			},
		}

		codec := &checks.StatusTableCodec{Wide: true}
		var buf bytes.Buffer
		err := codec.Encode(&buf, wideResults)
		if err != nil {
			t.Fatalf("Encode() error = %v", err)
		}

		output := buf.String()

		// Wide table must have PROBES column.
		if !strings.Contains(output, "PROBES") {
			t.Errorf("wide table should have PROBES column:\n%s", output)
		}
		if !strings.Contains(output, "Oregon") {
			t.Errorf("wide table should show probe name Oregon:\n%s", output)
		}
		if !strings.Contains(output, "Paris (offline)") {
			t.Errorf("wide table should show Paris (offline):\n%s", output)
		}
	})
}

func TestStatusTableCodec_InvalidType(t *testing.T) {
	codec := &checks.StatusTableCodec{}
	var buf bytes.Buffer
	err := codec.Encode(&buf, "invalid")
	if err == nil {
		t.Error("expected error for invalid data type")
	}
}

// ---------------------------------------------------------------------------
// BuildCheckStatusResults
// ---------------------------------------------------------------------------

func TestBuildCheckStatusResults(t *testing.T) {
	tests := []struct {
		name       string
		checks     []checks.Check
		successMap map[string]float64
		probeMap   map[string]float64
		wantLen    int
		verify     func(t *testing.T, results []checks.CheckStatusResult)
	}{
		{
			name: "check with metrics gets OK status",
			checks: []checks.Check{
				{ID: 1, Job: "check-1", Target: "https://example.com", Probes: []int64{1, 2, 3}, Settings: checks.CheckSettings{"http": map[string]any{}}},
			},
			successMap: map[string]float64{"check-1/https://example.com": 0.95},
			probeMap:   map[string]float64{"check-1/https://example.com": 3},
			wantLen:    1,
			verify: func(t *testing.T, results []checks.CheckStatusResult) {
				t.Helper()
				r := results[0]
				if r.Status != "OK" {
					t.Errorf("expected status OK, got %s", r.Status)
				}
				if r.Success == nil || *r.Success != 0.95 {
					t.Errorf("expected success 0.95, got %v", r.Success)
				}
				if r.ProbesUp != 3 {
					t.Errorf("expected probesUp 3, got %d", r.ProbesUp)
				}
				if r.ProbesTotal != 3 {
					t.Errorf("expected probesTotal 3, got %d", r.ProbesTotal)
				}
			},
		},
		{
			name: "check without metrics gets NODATA status",
			checks: []checks.Check{
				{ID: 2, Job: "check-2", Target: "https://nodata.com", Probes: []int64{1}, Settings: checks.CheckSettings{"ping": map[string]any{}}},
			},
			successMap: map[string]float64{},
			probeMap:   map[string]float64{},
			wantLen:    1,
			verify: func(t *testing.T, results []checks.CheckStatusResult) {
				t.Helper()
				r := results[0]
				if r.Status != "NODATA" {
					t.Errorf("expected status NODATA, got %s", r.Status)
				}
				if r.Success != nil {
					t.Errorf("expected nil success, got %v", *r.Success)
				}
			},
		},
		{
			name: "check with zero success gets FAILING status",
			checks: []checks.Check{
				{ID: 3, Job: "check-3", Target: "https://failing.com", Probes: []int64{1}, Settings: checks.CheckSettings{"http": map[string]any{}}},
			},
			successMap: map[string]float64{"check-3/https://failing.com": 0.0},
			probeMap:   map[string]float64{"check-3/https://failing.com": 1},
			wantLen:    1,
			verify: func(t *testing.T, results []checks.CheckStatusResult) {
				t.Helper()
				r := results[0]
				if r.Status != "FAILING" {
					t.Errorf("expected status FAILING, got %s", r.Status)
				}
			},
		},
		{
			name: "medium sensitivity 60% success gets FAILING status",
			checks: []checks.Check{
				{ID: 5, Job: "check-5", Target: "https://degraded.com", Probes: []int64{1, 2}, AlertSensitivity: "medium", Settings: checks.CheckSettings{"http": map[string]any{}}},
			},
			successMap: map[string]float64{"check-5/https://degraded.com": 0.6},
			probeMap:   map[string]float64{"check-5/https://degraded.com": 2},
			wantLen:    1,
			verify: func(t *testing.T, results []checks.CheckStatusResult) {
				t.Helper()
				if results[0].Status != "FAILING" {
					t.Errorf("expected status FAILING, got %s", results[0].Status)
				}
			},
		},
		{
			name: "high sensitivity 94% success gets FAILING status",
			checks: []checks.Check{
				{ID: 6, Job: "check-6", Target: "https://almost-ok.com", Probes: []int64{1}, AlertSensitivity: "high", Settings: checks.CheckSettings{"http": map[string]any{}}},
			},
			successMap: map[string]float64{"check-6/https://almost-ok.com": 0.94},
			probeMap:   map[string]float64{"check-6/https://almost-ok.com": 1},
			wantLen:    1,
			verify: func(t *testing.T, results []checks.CheckStatusResult) {
				t.Helper()
				if results[0].Status != "FAILING" {
					t.Errorf("expected status FAILING, got %s", results[0].Status)
				}
			},
		},
		{
			name: "low sensitivity 76% success gets OK status",
			checks: []checks.Check{
				{ID: 7, Job: "check-7", Target: "https://low-sens.com", Probes: []int64{1}, AlertSensitivity: "low", Settings: checks.CheckSettings{"http": map[string]any{}}},
			},
			successMap: map[string]float64{"check-7/https://low-sens.com": 0.76},
			probeMap:   map[string]float64{"check-7/https://low-sens.com": 1},
			wantLen:    1,
			verify: func(t *testing.T, results []checks.CheckStatusResult) {
				t.Helper()
				if results[0].Status != "OK" {
					t.Errorf("expected status OK, got %s", results[0].Status)
				}
			},
		},
		{
			name: "low sensitivity 74% success gets FAILING status",
			checks: []checks.Check{
				{ID: 8, Job: "check-8", Target: "https://low-fail.com", Probes: []int64{1}, AlertSensitivity: "low", Settings: checks.CheckSettings{"http": map[string]any{}}},
			},
			successMap: map[string]float64{"check-8/https://low-fail.com": 0.74},
			probeMap:   map[string]float64{"check-8/https://low-fail.com": 1},
			wantLen:    1,
			verify: func(t *testing.T, results []checks.CheckStatusResult) {
				t.Helper()
				if results[0].Status != "FAILING" {
					t.Errorf("expected status FAILING, got %s", results[0].Status)
				}
			},
		},
		{
			name: "default sensitivity 89% success gets FAILING status",
			checks: []checks.Check{
				{ID: 9, Job: "check-9", Target: "https://no-sens.com", Probes: []int64{1}, Settings: checks.CheckSettings{"http": map[string]any{}}},
			},
			successMap: map[string]float64{"check-9/https://no-sens.com": 0.89},
			probeMap:   map[string]float64{"check-9/https://no-sens.com": 1},
			wantLen:    1,
			verify: func(t *testing.T, results []checks.CheckStatusResult) {
				t.Helper()
				if results[0].Status != "FAILING" {
					t.Errorf("expected status FAILING, got %s", results[0].Status)
				}
			},
		},
		{
			name: "probe names populated from probe map",
			checks: []checks.Check{
				{ID: 4, Job: "check-4", Target: "https://probes.com", Probes: []int64{10, 20}, Settings: checks.CheckSettings{"http": map[string]any{}}},
			},
			successMap: map[string]float64{"check-4/https://probes.com": 0.9},
			probeMap:   map[string]float64{"check-4/https://probes.com": 2},
			wantLen:    1,
			verify: func(t *testing.T, results []checks.CheckStatusResult) {
				t.Helper()
				r := results[0]
				if len(r.ProbeNames) != 2 {
					t.Errorf("expected 2 probe names, got %d: %v", len(r.ProbeNames), r.ProbeNames)
					return
				}
				if r.ProbeNames[0] != "Oregon" {
					t.Errorf("expected probe name Oregon, got %s", r.ProbeNames[0])
				}
				if r.ProbeNames[1] != "Paris (offline)" {
					t.Errorf("expected probe name Paris (offline), got %s", r.ProbeNames[1])
				}
			},
		},
	}

	probeNameMap := map[int64]string{
		10: "Oregon",
		20: "Paris (offline)",
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := checks.BuildCheckStatusResults(tt.checks, tt.successMap, tt.probeMap, probeNameMap)
			if len(results) != tt.wantLen {
				t.Fatalf("expected %d results, got %d", tt.wantLen, len(results))
			}
			if tt.verify != nil {
				tt.verify(t, results)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Timeline table codec
// ---------------------------------------------------------------------------

func TestTimelineTableCodec_Encode(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name        string
		payload     any
		wantErr     bool
		wantContent []string
	}{
		{
			name: "valid payload renders table with header and rows",
			payload: checks.CheckTimelinePayload{
				Check: checks.Check{ID: 1, Job: "check-1", Target: "https://example.com"},
				Series: []checks.TimelineSeries{
					{
						Probe: "probe-a",
						Points: []checks.TimelinePoint{
							{Time: now.Add(-2 * time.Minute), Value: 1.0},
							{Time: now.Add(-1 * time.Minute), Value: 0.0},
						},
					},
				},
				Start: now.Add(-3 * time.Minute),
				End:   now,
			},
			wantErr:     false,
			wantContent: []string{"PROBE", "TIMESTAMP", "SUCCESS", "probe-a"},
		},
		{
			name:    "wrong type returns error",
			payload: 42,
			wantErr: true,
		},
	}

	codec := &checks.TimelineTableCodec{}

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

// ---------------------------------------------------------------------------
// ParseWindow
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// parseCheckTimelineTime
// ---------------------------------------------------------------------------

func TestParseCheckTimelineTime(t *testing.T) {
	now := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		input   string
		want    time.Time
		wantErr bool
	}{
		{name: "now", input: "now", want: now},
		{name: "now-6h", input: "now-6h", want: now.Add(-6 * time.Hour)},
		{name: "now-7d", input: "now-7d", want: now.Add(-7 * 24 * time.Hour)},
		{name: "empty defaults to now", input: "", want: now},
		{name: "RFC3339", input: "2026-03-01T00:00:00Z", want: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)},
		{name: "invalid", input: "garbage", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := checks.ParseCheckTimelineTime(tt.input, now)
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
				t.Errorf("ParseCheckTimelineTime(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseWindow(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    time.Duration
		wantErr bool
	}{
		{name: "6h", input: "6h", want: 6 * time.Hour},
		{name: "1h", input: "1h", want: 1 * time.Hour},
		{name: "24h", input: "24h", want: 24 * time.Hour},
		{name: "30m", input: "30m", want: 30 * time.Minute},
		{name: "7d", input: "7d", want: 7 * 24 * time.Hour},
		{name: "invalid", input: "abc", wantErr: true},
		{name: "empty", input: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := checks.ParseWindow(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseWindow(%q) error = %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("ParseWindow(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseCheckTimeRange(t *testing.T) {
	now := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name         string
		fromToSet    bool
		from, to     string
		window       string
		checkCreated float64
		wantStart    time.Time
		wantEnd      time.Time
		wantClamped  bool
		wantErr      bool
	}{
		{
			name:         "window only, no clamping (old check)",
			window:       "6h",
			checkCreated: float64(now.Add(-10 * time.Hour).Unix()),
			wantStart:    now.Add(-6 * time.Hour),
			wantEnd:      now,
			wantClamped:  false,
		},
		{
			name:         "window clamped to checkCreated (new check)",
			window:       "6h",
			checkCreated: float64(now.Add(-2 * time.Hour).Unix()),
			wantStart:    now.Add(-2 * time.Hour),
			wantEnd:      now,
			wantClamped:  true,
		},
		{
			name:         "checkCreated=0 means no clamping",
			window:       "6h",
			checkCreated: 0,
			wantStart:    now.Add(-6 * time.Hour),
			wantEnd:      now,
			wantClamped:  false,
		},
		{
			name:         "explicit --from/--to never clamps",
			fromToSet:    true,
			from:         "now-12h",
			to:           "now",
			checkCreated: float64(now.Add(-1 * time.Hour).Unix()),
			wantStart:    now.Add(-12 * time.Hour),
			wantEnd:      now,
			wantClamped:  false,
		},
		{
			name:    "invalid window",
			window:  "abc",
			wantErr: true,
		},
		{
			name:      "invalid --from",
			fromToSet: true,
			from:      "garbage",
			to:        "now",
			wantErr:   true,
		},
		{
			name:         "clock skew: created after now errors",
			window:       "6h",
			checkCreated: float64(now.Add(1 * time.Hour).Unix()),
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end, clamped, err := checks.ParseCheckTimeRange(
				tt.fromToSet, tt.from, tt.to, tt.window, now, tt.checkCreated,
			)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !start.Equal(tt.wantStart) {
				t.Errorf("start = %v, want %v", start, tt.wantStart)
			}
			if !end.Equal(tt.wantEnd) {
				t.Errorf("end = %v, want %v", end, tt.wantEnd)
			}
			if clamped != tt.wantClamped {
				t.Errorf("clamped = %v, want %v", clamped, tt.wantClamped)
			}
		})
	}
}
