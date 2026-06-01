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
			// --env is intentionally NOT a PromQL matcher (filtered post-parse).
			name: "env alone produces no matchers",
			opts: listOpts{Env: "production"},
			want: nil,
		},
		{
			name: "language combined with raw filter (env applied post-parse)",
			opts: listOpts{
				Filters:  []string{`k8s_namespace_name="prod"`},
				Language: "go",
				Env:      "production",
			},
			want: []Matcher{
				{Label: "k8s_namespace_name", Op: "=", Value: "prod"},
				{Label: "telemetry_sdk_language", Op: "=", Value: "go"},
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
		got := resolveItems(instrAll, instrumented, instrumented, graph)
		if len(got) != 4 {
			t.Fatalf("got %d, want 4: %+v", len(got), got)
		}
	})

	t.Run("instrumented returns target_info only", func(t *testing.T) {
		got := resolveItems(instrInstrumented, instrumented, instrumented, graph)
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
		got := resolveItems(instrUninstrumented, instrumented, instrumented, graph)
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

	// Regression: with --language go set, the display set narrows to just
	// `checkout` (Go), but `payments` is still in the *baseline* and must
	// not appear in the uninstrumented bucket.
	t.Run("baseline keeps non-display instrumented out of uninstrumented", func(t *testing.T) {
		display := []Service{
			{Name: "checkout", Language: "go", Instrumented: true},
		}
		baseline := instrumented // full unfiltered set

		all := resolveItems(instrAll, display, baseline, graph)
		for _, s := range all {
			if s.Name == "payments" && !s.Instrumented {
				t.Errorf("payments leaked into uninstrumented bucket in --instrumentation all")
			}
		}

		un := resolveItems(instrUninstrumented, display, baseline, graph)
		for _, s := range un {
			if s.Name == "payments" {
				t.Errorf("payments leaked into uninstrumented bucket in --instrumentation uninstrumented")
			}
		}
	})
}

func TestFilterByEnv(t *testing.T) {
	items := []Service{
		{Name: "a", Labels: map[string]string{"deployment_environment": "prod"}},
		{Name: "b", Labels: map[string]string{"deployment_environment_name": "prod"}}, // newer-semconv only
		{Name: "c", Labels: map[string]string{"deployment_environment": "staging"}},
		{Name: "d"}, // no env at all
	}
	got := filterByEnv(items, "prod")
	if len(got) != 2 {
		t.Fatalf("got %d, want 2: %+v", len(got), got)
	}
	if got[0].Name != "a" || got[1].Name != "b" {
		t.Errorf("want a + b, got %+v", got)
	}
	// Empty filter = passthrough.
	if all := filterByEnv(items, ""); len(all) != len(items) {
		t.Errorf("empty filter should passthrough, got %d", len(all))
	}
}
