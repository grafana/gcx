package dashboards_test

import (
	"strings"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/dashboards"
)

func TestSnapshotOpts_Validate(t *testing.T) {
	tests := []struct {
		name        string
		opts        dashboards.SnapshotOptsForTest
		wantErr     bool
		errContains string
		wantFrom    string
		wantTo      string
		wantWidth   int
		wantHeight  int
	}{
		{
			name:        "window with from is an error",
			opts:        dashboards.SnapshotOptsForTest{Theme: "dark", Window: "6h", From: "now-2h"},
			wantErr:     true,
			errContains: "--window is mutually exclusive",
		},
		{
			name:        "window with to is an error",
			opts:        dashboards.SnapshotOptsForTest{Theme: "dark", Window: "6h", To: "now"},
			wantErr:     true,
			errContains: "--window is mutually exclusive",
		},
		{
			name:        "window with both from and to is an error",
			opts:        dashboards.SnapshotOptsForTest{Theme: "dark", Window: "6h", From: "now-2h", To: "now"},
			wantErr:     true,
			errContains: "--window is mutually exclusive",
		},
		{
			name:        "invalid theme is an error",
			opts:        dashboards.SnapshotOptsForTest{Theme: "purple"},
			wantErr:     true,
			errContains: "--theme must be",
		},
		{
			name:        "empty theme is an error",
			opts:        dashboards.SnapshotOptsForTest{Theme: ""},
			wantErr:     true,
			errContains: "--theme must be",
		},
		{
			name:       "window alone expands to from/to",
			opts:       dashboards.SnapshotOptsForTest{Theme: "dark", Window: "6h"},
			wantErr:    false,
			wantFrom:   "now-6h",
			wantTo:     "now",
			wantWidth:  1920,
			wantHeight: -1,
		},
		{
			name:       "window with different value",
			opts:       dashboards.SnapshotOptsForTest{Theme: "dark", Window: "7d"},
			wantErr:    false,
			wantFrom:   "now-7d",
			wantTo:     "now",
			wantWidth:  1920,
			wantHeight: -1,
		},
		{
			name:       "no flags sets full dashboard defaults (dark theme)",
			opts:       dashboards.SnapshotOptsForTest{Theme: "dark"},
			wantErr:    false,
			wantWidth:  1920,
			wantHeight: -1,
		},
		{
			name:       "light theme is valid",
			opts:       dashboards.SnapshotOptsForTest{Theme: "light"},
			wantErr:    false,
			wantWidth:  1920,
			wantHeight: -1,
		},
		{
			name:       "panel flag sets panel defaults",
			opts:       dashboards.SnapshotOptsForTest{Theme: "dark", PanelID: 42},
			wantErr:    false,
			wantWidth:  800,
			wantHeight: 600,
		},
		{
			name:       "explicit width/height preserved for full dashboard",
			opts:       dashboards.SnapshotOptsForTest{Theme: "dark", Width: 1000, Height: 500},
			wantErr:    false,
			wantWidth:  1000,
			wantHeight: 500,
		},
		{
			name:       "explicit width/height preserved for panel",
			opts:       dashboards.SnapshotOptsForTest{Theme: "dark", PanelID: 42, Width: 400, Height: 300},
			wantErr:    false,
			wantWidth:  400,
			wantHeight: 300,
		},
		{
			name:       "from and to without window is valid",
			opts:       dashboards.SnapshotOptsForTest{Theme: "dark", From: "now-1h", To: "now"},
			wantErr:    false,
			wantFrom:   "now-1h",
			wantTo:     "now",
			wantWidth:  1920,
			wantHeight: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.opts.Validate()

			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantErr {
				if tt.errContains != "" && err != nil {
					if !strings.Contains(err.Error(), tt.errContains) {
						t.Errorf("Validate() error = %q, want it to contain %q", err.Error(), tt.errContains)
					}
				}
				return
			}

			if tt.wantFrom != "" && tt.opts.From != tt.wantFrom {
				t.Errorf("Validate() From = %q, want %q", tt.opts.From, tt.wantFrom)
			}
			if tt.wantTo != "" && tt.opts.To != tt.wantTo {
				t.Errorf("Validate() To = %q, want %q", tt.opts.To, tt.wantTo)
			}
			if tt.wantWidth != 0 && tt.opts.Width != tt.wantWidth {
				t.Errorf("Validate() Width = %d, want %d", tt.opts.Width, tt.wantWidth)
			}
			if tt.wantHeight != 0 && tt.opts.Height != tt.wantHeight {
				t.Errorf("Validate() Height = %d, want %d", tt.opts.Height, tt.wantHeight)
			}
		})
	}
}
