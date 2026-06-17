package dashboards_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/providers/dashboards"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestListOptsDefaultLimit(t *testing.T) {
	t.Cleanup(agent.ResetForTesting)
	t.Setenv("GCX_AGENT_MODE", "false")
	agent.ResetForTesting()

	opts := dashboards.NewListOptsForTest(pflag.NewFlagSet("list", pflag.ContinueOnError))

	if opts.Limit != dashboards.DefaultDashboardListLimitForTest {
		t.Fatalf("default limit = %d, want %d", opts.Limit, dashboards.DefaultDashboardListLimitForTest)
	}
}

func TestListOptsValidateContinueRequiresLimit(t *testing.T) {
	t.Cleanup(agent.ResetForTesting)
	t.Setenv("GCX_AGENT_MODE", "false")
	agent.ResetForTesting()

	opts := dashboards.NewListOptsForTest(pflag.NewFlagSet("list", pflag.ContinueOnError))
	opts.Limit = 0
	opts.ContinueToken = "next-page"

	err := opts.Validate()
	if err == nil {
		t.Fatal("Validate() returned nil, want error")
	}
	if !strings.Contains(err.Error(), "--continue requires --limit") {
		t.Fatalf("Validate() error = %q, want --continue/--limit guidance", err.Error())
	}
}

func TestEmitListPaginationHint(t *testing.T) {
	t.Cleanup(agent.ResetForTesting)
	t.Setenv("GCX_AGENT_MODE", "false")
	agent.ResetForTesting()

	list := &unstructured.UnstructuredList{
		Items: []unstructured.Unstructured{
			{Object: map[string]any{"metadata": map[string]any{"name": "dash-1"}}},
			{Object: map[string]any{"metadata": map[string]any{"name": "dash-2"}}},
		},
	}
	list.SetContinue("token-abc")

	var buf bytes.Buffer
	opts := dashboards.NewListOptsForTest(pflag.NewFlagSet("list", pflag.ContinueOnError))
	opts.Limit = 50
	dashboards.EmitListPaginationHintForTest(&buf,
		[]string{"gcx", "--context", "dev", "dashboards", "list", "--api-version=dashboard.grafana.app/v2", "--limit", "50"},
		list,
		opts,
	)

	out := buf.String()
	for _, want := range []string{
		"hint: showing 2 dashboards; more pages are available",
		"gcx --context dev dashboards list",
		"--api-version=dashboard.grafana.app/v2",
		"--limit 50",
		"--continue token-abc",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("pagination hint = %q, want substring %q", out, want)
		}
	}
}

func TestEmitListPaginationHintNoContinueToken(t *testing.T) {
	list := &unstructured.UnstructuredList{}

	var buf bytes.Buffer
	opts := dashboards.NewListOptsForTest(pflag.NewFlagSet("list", pflag.ContinueOnError))
	dashboards.EmitListPaginationHintForTest(&buf, []string{"gcx", "dashboards", "list"}, list, opts)
	if buf.Len() != 0 {
		t.Fatalf("pagination hint output = %q, want empty", buf.String())
	}
}

func TestEmitListPaginationHintStructuredOutput(t *testing.T) {
	list := &unstructured.UnstructuredList{}
	list.SetContinue("token-abc")

	for _, outputFormat := range []string{"json", "yaml", "agents"} {
		t.Run(outputFormat, func(t *testing.T) {
			var buf bytes.Buffer
			opts := dashboards.NewListOptsForTest(pflag.NewFlagSet("list", pflag.ContinueOnError))
			opts.IO.OutputFormat = outputFormat

			dashboards.EmitListPaginationHintForTest(&buf, []string{"gcx", "dashboards", "list"}, list, opts)
			if buf.Len() != 0 {
				t.Fatalf("pagination hint output = %q, want empty", buf.String())
			}
		})
	}
}
