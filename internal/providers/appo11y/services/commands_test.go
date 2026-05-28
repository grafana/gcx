package services //nolint:testpackage // Tests cover unexported buildFilters and Validate behaviour.

import (
	"testing"
)

func TestBuildFilters(t *testing.T) {
	tests := []struct {
		name string
		opts listOpts
		want []string
	}{
		{
			name: "empty",
			opts: listOpts{Metric: defaultTargetInfoMetric},
			want: nil,
		},
		{
			name: "language only",
			opts: listOpts{Metric: defaultTargetInfoMetric, Language: "go"},
			want: []string{`telemetry_sdk_language="go"`},
		},
		{
			name: "env only",
			opts: listOpts{Metric: defaultTargetInfoMetric, Env: "production"},
			want: []string{`deployment_environment="production"`},
		},
		{
			name: "language and env combined with raw filter",
			opts: listOpts{
				Metric:   defaultTargetInfoMetric,
				Filters:  []string{`k8s_namespace_name="prod"`},
				Language: "go",
				Env:      "production",
			},
			want: []string{
				`k8s_namespace_name="prod"`,
				`telemetry_sdk_language="go"`,
				`deployment_environment="production"`,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.opts.buildFilters()
			if err != nil {
				t.Fatalf("buildFilters() err = %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d filters, want %d: %v", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("filter[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestListOptsValidate(t *testing.T) {
	// json is always a registered format; setting it here lets us exercise the
	// services-specific Validate checks without standing up the full codec setup.
	mk := func(o listOpts) listOpts {
		o.IO.OutputFormat = "json"
		return o
	}
	tests := []struct {
		name    string
		opts    listOpts
		wantErr bool
	}{
		{name: "ok", opts: mk(listOpts{Metric: defaultTargetInfoMetric})},
		{name: "blank metric rejected", opts: mk(listOpts{Metric: "   "}), wantErr: true},
		{name: "negative limit rejected", opts: mk(listOpts{Metric: defaultTargetInfoMetric, Limit: -1}), wantErr: true},
		{name: "zero limit ok (unlimited)", opts: mk(listOpts{Metric: defaultTargetInfoMetric, Limit: 0})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.opts.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() err = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}
