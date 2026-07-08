package services //nolint:testpackage // Tests cover unexported helpers and the table codec.

import (
	"bytes"
	"strings"
	"testing"
)

func TestFilterToLabel(t *testing.T) {
	items := []LabelSummary{
		{Name: "k8s_cluster_name", Cardinality: 2, Values: []string{"east", "west"}},
		{Name: "span_name", Cardinality: 5, Values: []string{"a"}},
	}
	got := filterToLabel(items, "k8s_cluster_name")
	if len(got) != 1 || got[0].Name != "k8s_cluster_name" || len(got[0].Values) != 2 {
		t.Fatalf("filterToLabel wrong: %+v", got)
	}
	if got := filterToLabel(items, "nope"); got != nil {
		t.Errorf("missing label should return nil, got %+v", got)
	}
}

func TestSampleValues(t *testing.T) {
	// No values → dash.
	if got := sampleValues(LabelSummary{Name: "x"}); got != "-" {
		t.Errorf("empty = %q, want -", got)
	}
	// All values present → plain join, no "more".
	if got := sampleValues(LabelSummary{Cardinality: 2, Values: []string{"a", "b"}}); got != "a, b" {
		t.Errorf("got %q, want 'a, b'", got)
	}
	// Sample smaller than cardinality → "+N more".
	got := sampleValues(LabelSummary{Cardinality: 5, Values: []string{"a", "b"}})
	if !strings.Contains(got, "a, b") || !strings.Contains(got, "+3 more") {
		t.Errorf("got %q, want a,b + '+3 more'", got)
	}
}

func TestLabelsTableCodec_Summary(t *testing.T) {
	resp := &ServiceLabelsResponse{
		Service: Service{Name: "checkout", Namespace: "billing"},
		Metric:  "traces_span_metrics_calls_total",
		Window:  "1h",
		Items: []LabelSummary{
			{Name: "k8s_cluster_name", Cardinality: 3, Values: []string{"a", "b"}},
		},
	}
	var buf bytes.Buffer
	c := &labelsTableCodec{opts: &labelsOpts{}}
	if err := c.Encode(&buf, resp); err != nil {
		t.Fatalf("encode err = %v", err)
	}
	out := buf.String()
	for _, want := range []string{"LABEL", "CARDINALITY", "SAMPLE VALUES", "k8s_cluster_name", "+1 more"} {
		if !strings.Contains(out, want) {
			t.Errorf("summary output missing %q:\n%s", want, out)
		}
	}
}

func TestLabelsTableCodec_LabelDrilldown(t *testing.T) {
	resp := &ServiceLabelsResponse{
		Service: Service{Name: "checkout", Namespace: "billing"},
		Items: []LabelSummary{
			{Name: "k8s_cluster_name", Cardinality: 2, Values: []string{"east", "west"}},
		},
	}
	var buf bytes.Buffer
	c := &labelsTableCodec{opts: &labelsOpts{Label: "k8s_cluster_name"}}
	if err := c.Encode(&buf, resp); err != nil {
		t.Fatalf("encode err = %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "east") || !strings.Contains(out, "west") {
		t.Errorf("drill-down missing values:\n%s", out)
	}
	// Summary-only headers must NOT appear in the value list.
	if strings.Contains(out, "CARDINALITY") {
		t.Errorf("drill-down should not show CARDINALITY header:\n%s", out)
	}
}
