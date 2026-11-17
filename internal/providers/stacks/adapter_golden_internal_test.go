package stacks

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

func TestStackAdapterGolden(t *testing.T) {
	testutils.SetAgentMode(t, false)
	stack := cloud.StackInfo{Slug: "example", Name: "Example", Status: "active", RegionSlug: "eu", URL: "https://example.grafana.net", PlanName: "Pro", DeleteProtection: true, CreatedAt: "2026-10-01T12:00:00Z"}
	for _, name := range []string{"table", "wide"} {
		for _, tc := range []struct {
			name  string
			items []cloud.StackInfo
		}{{"rows", []cloud.StackInfo{stack, {Slug: "dev", Name: "Development", CreatedAt: "short"}}}, {"empty", nil}} {
			t.Run("list_"+name+"_"+tc.name, func(t *testing.T) {
				o := &listOpts{Org: "example"}
				f := pflag.NewFlagSet("list", pflag.ContinueOnError)
				o.setup(f)
				require.NoError(t, f.Set("org", "example"))
				require.NoError(t, f.Set("output", name))
				require.NoError(t, o.Validate())
				var buf bytes.Buffer
				require.NoError(t, o.IO.Encode(&buf, tc.items))
				testutils.Golden(t, "list_"+name+"_"+tc.name, buf.String())
			})
		}
		t.Run("get_"+name, func(t *testing.T) {
			o := &getOpts{}
			f := pflag.NewFlagSet("get", pflag.ContinueOnError)
			o.setup(f)
			require.NoError(t, f.Set("output", name))
			require.NoError(t, o.IO.Validate())
			var buf bytes.Buffer
			require.NoError(t, o.IO.Encode(&buf, stack))
			testutils.Golden(t, "get_"+name, buf.String())
		})
	}
	for _, tc := range []struct {
		name  string
		value any
	}{
		{"created", createdStackSummary{Name: "Example", OrgSlug: "example-org", Slug: "example", Status: "active", URL: "https://example.grafana.net"}},
		{"dry_run", newDryRunPreview("created", "POST", "/api/instances", map[string]string{"name": "Example"})},
		{"updated", stack},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			require.NoError(t, (&stackTableCodec{}).Encode(&buf, tc.value))
			testutils.Golden(t, tc.name+"_table", buf.String())
		})
	}
}
