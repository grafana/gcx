package auth_test

import (
	"testing"

	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/providers/adaptive/auth"
)

func TestExtractSignalInfo(t *testing.T) {
	stack := cloud.StackInfo{
		HMInstancePromURL: "https://prometheus-prod-01-eu-west-0.grafana.net",
		HMInstancePromID:  12345,
		HLInstanceURL:     "https://logs-prod-eu-west-0.grafana.net",
		HLInstanceID:      67890,
		HTInstanceURL:     "https://tempo-prod-eu-west-0.grafana.net",
		HTInstanceID:      11111,
	}

	tests := []struct {
		name       string
		signal     string
		wantURL    string
		wantID     int
		wantErrMsg string
	}{
		{
			name:    "metrics",
			signal:  "metrics",
			wantURL: "https://prometheus-prod-01-eu-west-0.grafana.net",
			wantID:  12345,
		},
		{
			name:    "logs",
			signal:  "logs",
			wantURL: "https://logs-prod-eu-west-0.grafana.net",
			wantID:  67890,
		},
		{
			name:    "traces",
			signal:  "traces",
			wantURL: "https://tempo-prod-eu-west-0.grafana.net",
			wantID:  11111,
		},
		{
			name:       "unknown signal",
			signal:     "profiles",
			wantErrMsg: "unknown signal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url, id, err := auth.ExtractSignalInfo(stack, tt.signal)
			if tt.wantErrMsg != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErrMsg)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if url != tt.wantURL {
				t.Errorf("URL = %q, want %q", url, tt.wantURL)
			}
			if id != tt.wantID {
				t.Errorf("ID = %d, want %d", id, tt.wantID)
			}
		})
	}
}

func TestExtractSignalInfoMissingURL(t *testing.T) {
	stack := cloud.StackInfo{
		HMInstancePromID: 12345,
	}
	_, _, err := auth.ExtractSignalInfo(stack, "metrics")
	if err == nil {
		t.Fatal("expected error for missing URL, got nil")
	}
}

func TestExtractSignalInfoMissingID(t *testing.T) {
	stack := cloud.StackInfo{
		HMInstancePromURL: "https://prometheus.grafana.net",
	}
	_, _, err := auth.ExtractSignalInfo(stack, "metrics")
	if err == nil {
		t.Fatal("expected error for missing ID, got nil")
	}
}
