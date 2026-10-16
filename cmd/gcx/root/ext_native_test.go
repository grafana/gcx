//go:build !wasip1

package root_test

import (
	"slices"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/root"
)

func TestExtensionArgumentBoundary(t *testing.T) {
	for _, tc := range []struct{ args, want []string }{
		{[]string{"ext", "sample", "--help"}, []string{"ext", "sample", "--", "--help"}},
		{[]string{"--context", "ext", "ext", "sample", "--agent=false"}, []string{"--context", "ext", "ext", "sample", "--", "--agent=false"}},
		{[]string{"ext", "sample", "--", "--context", "child"}, []string{"ext", "sample", "--", "--", "--context", "child"}},
		{[]string{"ext", "install", "source", "--yes"}, []string{"ext", "install", "source", "--yes"}},
		{[]string{"resources", "get", "dashboards"}, []string{"resources", "get", "dashboards"}},
	} {
		if got := root.RewriteExtensionArgs(root.Command("test"), tc.args); !slices.Equal(got, tc.want) {
			t.Fatalf("args=%v got=%v want=%v", tc.args, got, tc.want)
		}
	}
}
