package services //nolint:testpackage // Tests cover unexported fleet command opts/helpers.

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestFleetOperationsListOptsValidate(t *testing.T) {
	mk := func(o fleetOperationsListOpts) fleetOperationsListOpts {
		o.IO.OutputFormat = "json"
		if o.Since == "" {
			o.Since = defaultRedWindow
		}
		if o.Kind == "" {
			o.Kind = "inbound"
		}
		if o.KG.Mode == "" {
			o.KG.Mode = string(kgModeAuto)
		}
		if o.Limit == 0 {
			o.Limit = fleetOperationsDefaultLimit
		}
		return o
	}
	tests := []struct {
		name    string
		opts    fleetOperationsListOpts
		wantErr bool
	}{
		{name: "defaults ok", opts: mk(fleetOperationsListOpts{})},
		{name: "limit zero rejected", opts: func() fleetOperationsListOpts {
			o := mk(fleetOperationsListOpts{})
			o.Limit = 0
			return o
		}(), wantErr: true},
		{name: "limit negative rejected", opts: func() fleetOperationsListOpts {
			o := mk(fleetOperationsListOpts{})
			o.Limit = -1
			return o
		}(), wantErr: true},
		{name: "limit above cap rejected", opts: func() fleetOperationsListOpts {
			o := mk(fleetOperationsListOpts{})
			o.Limit = 501
			return o
		}(), wantErr: true},
		{name: "limit at cap ok", opts: func() fleetOperationsListOpts {
			o := mk(fleetOperationsListOpts{})
			o.Limit = 500
			return o
		}()},
	}
	cmd := &cobra.Command{Use: "list"}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.opts.Validate(cmd)
			if (err != nil) != tt.wantErr {
				t.Errorf("err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestFleetOperationsListOptsValidate_LimitZeroIsRejected pins down the
// deliberate divergence from every other `list` command in this repo:
// --limit 0 means "unbounded" everywhere else, but is rejected here
// because the unbounded fleet shape is #services x #operations.
func TestFleetOperationsListOptsValidate_LimitZeroIsRejected(t *testing.T) {
	o := fleetOperationsListOpts{
		Since: defaultRedWindow, Kind: "inbound", MetricsMode: metricsModeAuto,
		Limit: 0, KG: kgFlags{Mode: string(kgModeAuto)},
	}
	o.IO.OutputFormat = "json"
	if err := o.Validate(&cobra.Command{Use: "list"}); err == nil {
		t.Error("expected --limit 0 to be rejected")
	}
}

func TestNewFleetOperationsListCommand_RegistersLimitFlag(t *testing.T) {
	cmd := newFleetOperationsListCommand(nil)
	f := cmd.Flags().Lookup("limit")
	if f == nil {
		t.Fatal("expected --limit flag to be registered")
	}
	if !strings.Contains(f.Usage, "500") {
		t.Errorf("--limit usage should document the 500 cap: %q", f.Usage)
	}
}

func TestNewOperationGetCommand_RequiresService(t *testing.T) {
	cmd := newOperationGetCommand(nil)
	cmd.SetArgs([]string{"GET /cart"})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected an error when --service is not set")
	}
	if !strings.Contains(err.Error(), "service") {
		t.Errorf("error should mention the missing --service flag: %v", err)
	}
}

// TestAppendSpanNameMatcher_ThreadsIntoSingleServiceBuilder confirms the
// positional operation name reaches the existing single-service query
// builders as a span_name= matcher — `operations get` needs zero new
// PromQL, just this narrowing matcher on top of the per-service builders.
func TestAppendSpanNameMatcher_ThreadsIntoSingleServiceBuilder(t *testing.T) {
	v3, _ := metricNamesByMode(MetricsModeV3)
	matchers := appendSpanNameMatcher(nil, "GET /api/v1/carts")
	got, err := buildOperationsRateQuery(v3, "shop", "cart", "5m", []string{spanKindServer}, matchers, nil)
	if err != nil {
		t.Fatalf("build err = %v", err)
	}
	want := `sum by (span_name) (rate(traces_span_metrics_calls_total{job="shop/cart",span_kind=~"SPAN_KIND_SERVER",span_name="GET /api/v1/carts"}[5m]))`
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

// TestAppendSpanNameMatcher_Escapes confirms an operation name with a
// PromQL-special character (a literal quote) is escaped the same way as
// every other matcher value — no injection via the positional argument.
func TestAppendSpanNameMatcher_Escapes(t *testing.T) {
	v3, _ := metricNamesByMode(MetricsModeV3)
	matchers := appendSpanNameMatcher(nil, `x"} or up{`)
	got, err := buildOperationsRateQuery(v3, "shop", "cart", "5m", []string{spanKindServer}, matchers, nil)
	if err != nil {
		t.Fatalf("build err = %v", err)
	}
	if !strings.Contains(got, `span_name="x\"} or up{"`) {
		t.Errorf("operation name not escaped: %s", got)
	}
}

func TestFilterFleetByNamespaceAndEnv(t *testing.T) {
	items := []FleetOperation{
		{Service: "checkout", Namespace: "billing", Operation: Operation{Labels: map[string]string{"deployment_environment": "production"}}},
		{Service: "cart", Namespace: "shop", Operation: Operation{Labels: map[string]string{"deployment_environment": "staging"}}},
	}

	if got := filterFleetByNamespaceAndEnv(items, "", ""); len(got) != 2 {
		t.Fatalf("no filter: len = %d, want 2", len(got))
	}
	if got := filterFleetByNamespaceAndEnv(items, "billing", ""); len(got) != 1 || got[0].Service != "checkout" {
		t.Fatalf("namespace filter = %+v", got)
	}
	if got := filterFleetByNamespaceAndEnv(items, "", "staging"); len(got) != 1 || got[0].Service != "cart" {
		t.Fatalf("env filter = %+v", got)
	}
}
