package services //nolint:testpackage // Tests cover unexported buildFilters and Validate behaviour.

import (
	"testing"
)

func TestBuildFilters(t *testing.T) {
	tests := []struct {
		name string
		opts listOpts
		want []Matcher
	}{
		{
			name: "empty",
			opts: listOpts{},
			want: nil,
		},
		{
			name: "language only",
			opts: listOpts{Language: "go"},
			want: []Matcher{{Label: "telemetry_sdk_language", Op: "=", Value: "go"}},
		},
		{
			name: "env only",
			opts: listOpts{Env: "production"},
			want: []Matcher{{Label: "deployment_environment", Op: "=", Value: "production"}},
		},
		{
			name: "language and env combined with raw filter",
			opts: listOpts{
				Filters:  []string{`k8s_namespace_name="prod"`},
				Language: "go",
				Env:      "production",
			},
			want: []Matcher{
				{Label: "k8s_namespace_name", Op: "=", Value: "prod"},
				{Label: "telemetry_sdk_language", Op: "=", Value: "go"},
				{Label: "deployment_environment", Op: "=", Value: "production"},
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
				t.Fatalf("got %d matchers, want %d: %+v", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("matcher[%d] = %+v, want %+v", i, got[i], tt.want[i])
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
		if o.Instrumentation == "" {
			o.Instrumentation = instrAll
		}
		return o
	}
	tests := []struct {
		name    string
		opts    listOpts
		wantErr bool
	}{
		{name: "ok", opts: mk(listOpts{})},
		{name: "negative limit rejected", opts: mk(listOpts{Limit: -1}), wantErr: true},
		{name: "zero limit ok (unlimited)", opts: mk(listOpts{Limit: 0})},
		{name: "instrumented ok", opts: mk(listOpts{Instrumentation: instrInstrumented})},
		{name: "uninstrumented ok", opts: mk(listOpts{Instrumentation: instrUninstrumented})},
		{name: "bogus instrumentation rejected", opts: mk(listOpts{Instrumentation: "partial"}), wantErr: true},
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

func TestResolveItems(t *testing.T) {
	instrumented := []Service{
		{Name: "checkout", Language: "go", Instrumented: true},
		{Name: "payments", Language: "java", Instrumented: true},
	}
	graph := []Service{
		{Name: "payments"}, // overlaps with instrumented
		{Name: "legacy-billing"},
		{Name: "third-party"},
	}

	t.Run("all merges both", func(t *testing.T) {
		got := resolveItems(instrAll, instrumented, graph)
		if len(got) != 4 {
			t.Fatalf("got %d, want 4: %+v", len(got), got)
		}
	})

	t.Run("instrumented returns target_info only", func(t *testing.T) {
		got := resolveItems(instrInstrumented, instrumented, graph)
		if len(got) != 2 {
			t.Fatalf("got %d, want 2: %+v", len(got), got)
		}
		for _, s := range got {
			if !s.Instrumented {
				t.Errorf("%q should be instrumented", s.Name)
			}
		}
	})

	t.Run("uninstrumented drops names known to target_info", func(t *testing.T) {
		got := resolveItems(instrUninstrumented, instrumented, graph)
		if len(got) != 2 {
			t.Fatalf("got %d, want 2: %+v", len(got), got)
		}
		for _, s := range got {
			if s.Instrumented {
				t.Errorf("%q should be uninstrumented", s.Name)
			}
			if s.Name == "payments" {
				t.Errorf("payments leaked into uninstrumented set")
			}
		}
	})
}
