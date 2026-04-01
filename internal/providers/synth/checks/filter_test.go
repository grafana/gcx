package checks_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers/synth/checks"
)

func TestCheckFilter_MatchCheck(t *testing.T) {
	checkWithLabels := checks.Check{
		Job:    "shopk8s-grafana",
		Target: "https://grafana.example.com",
		Labels: []checks.Label{
			{Name: "env", Value: "prod"},
			{Name: "team", Value: "platform"},
		},
	}
	checkNoLabels := checks.Check{
		Job:    "ping-google",
		Target: "8.8.8.8",
	}

	tests := []struct {
		name   string
		filter *checks.CheckFilter
		check  checks.Check
		want   bool
	}{
		{
			name:   "nil filter matches all",
			filter: nil,
			check:  checkWithLabels,
			want:   true,
		},
		{
			name:   "empty filter matches all",
			filter: &checks.CheckFilter{},
			check:  checkWithLabels,
			want:   true,
		},
		{
			name:   "exact job match",
			filter: &checks.CheckFilter{JobPattern: "shopk8s-grafana"},
			check:  checkWithLabels,
			want:   true,
		},
		{
			name:   "glob job match",
			filter: &checks.CheckFilter{JobPattern: "shopk8s-*"},
			check:  checkWithLabels,
			want:   true,
		},
		{
			name:   "glob job no match",
			filter: &checks.CheckFilter{JobPattern: "shopk8s-*"},
			check:  checkNoLabels,
			want:   false,
		},
		{
			name:   "label match",
			filter: &checks.CheckFilter{Labels: map[string]string{"env": "prod"}},
			check:  checkWithLabels,
			want:   true,
		},
		{
			name:   "label no match - wrong value",
			filter: &checks.CheckFilter{Labels: map[string]string{"env": "staging"}},
			check:  checkWithLabels,
			want:   false,
		},
		{
			name:   "label no match - missing key",
			filter: &checks.CheckFilter{Labels: map[string]string{"zone": "us-east"}},
			check:  checkWithLabels,
			want:   false,
		},
		{
			name:   "multiple labels all match",
			filter: &checks.CheckFilter{Labels: map[string]string{"env": "prod", "team": "platform"}},
			check:  checkWithLabels,
			want:   true,
		},
		{
			name:   "multiple labels partial match fails",
			filter: &checks.CheckFilter{Labels: map[string]string{"env": "prod", "team": "backend"}},
			check:  checkWithLabels,
			want:   false,
		},
		{
			name: "combined job + label match",
			filter: &checks.CheckFilter{
				JobPattern: "shopk8s-*",
				Labels:     map[string]string{"env": "prod"},
			},
			check: checkWithLabels,
			want:  true,
		},
		{
			name: "combined job matches but label fails",
			filter: &checks.CheckFilter{
				JobPattern: "shopk8s-*",
				Labels:     map[string]string{"env": "staging"},
			},
			check: checkWithLabels,
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.filter.MatchCheck(tt.check)
			if got != tt.want {
				t.Errorf("MatchCheck() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCheckFilter_MatchResult(t *testing.T) {
	tests := []struct {
		name   string
		filter *checks.CheckFilter
		result checks.CheckStatusResult
		want   bool
	}{
		{
			name:   "nil filter matches all",
			filter: nil,
			result: checks.CheckStatusResult{Status: "FAILING"},
			want:   true,
		},
		{
			name:   "empty StatusStr matches all",
			filter: &checks.CheckFilter{},
			result: checks.CheckStatusResult{Status: "OK"},
			want:   true,
		},
		{
			name:   "exact status match",
			filter: &checks.CheckFilter{StatusStr: "OK"},
			result: checks.CheckStatusResult{Status: "OK"},
			want:   true,
		},
		{
			name:   "case-insensitive match",
			filter: &checks.CheckFilter{StatusStr: "ok"},
			result: checks.CheckStatusResult{Status: "OK"},
			want:   true,
		},
		{
			name:   "status no match",
			filter: &checks.CheckFilter{StatusStr: "FAILING"},
			result: checks.CheckStatusResult{Status: "OK"},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.filter.MatchResult(tt.result)
			if got != tt.want {
				t.Errorf("MatchResult() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseLabelFlags(t *testing.T) {
	tests := []struct {
		name    string
		input   []string
		want    map[string]string
		wantErr bool
	}{
		{
			name:  "empty input",
			input: nil,
			want:  map[string]string{},
		},
		{
			name:  "single label",
			input: []string{"env=prod"},
			want:  map[string]string{"env": "prod"},
		},
		{
			name:  "multiple labels",
			input: []string{"env=prod", "team=platform"},
			want:  map[string]string{"env": "prod", "team": "platform"},
		},
		{
			name:  "value with equals sign",
			input: []string{"expr=a=b"},
			want:  map[string]string{"expr": "a=b"},
		},
		{
			name:    "missing value with no equals",
			input:   []string{"env"},
			wantErr: true,
		},
		{
			name:    "empty key",
			input:   []string{"=value"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := checks.ParseLabelFlags(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Error("expected an error but got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Errorf("ParseLabelFlags() = %v, want %v", got, tt.want)
				return
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("ParseLabelFlags()[%q] = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

func TestCheckFilter_Validate(t *testing.T) {
	tests := []struct {
		name    string
		filter  *checks.CheckFilter
		wantErr bool
	}{
		{name: "nil filter", filter: nil},
		{name: "valid glob", filter: &checks.CheckFilter{JobPattern: "shopk8s-*"}},
		{name: "empty pattern", filter: &checks.CheckFilter{JobPattern: ""}},
		{name: "invalid glob", filter: &checks.CheckFilter{JobPattern: "[invalid"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.filter.Validate()
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
