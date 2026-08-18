package root_test

import (
	"slices"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/root"
)

func TestRewriteExtensionArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "extension flags are separated from gcx's",
			args: []string{"ext", "azure", "provision", "--dry-run"},
			want: []string{"ext", "azure", "--", "provision", "--dry-run"},
		},
		{
			name: "global flags before ext still parse as gcx's",
			args: []string{"--context", "prod", "ext", "azure", "--subscription", "x"},
			want: []string{"--context", "prod", "ext", "azure", "--", "--subscription", "x"},
		},
		{
			name: "bare extension name is separated too",
			args: []string{"ext", "azure"},
			want: []string{"ext", "azure", "--"},
		},
		{
			name: "known verbs are left alone",
			args: []string{"ext", "install", "./thing"},
			want: []string{"ext", "install", "./thing"},
		},
		{
			name: "list is left alone",
			args: []string{"ext", "list", "--output", "json"},
			want: []string{"ext", "list", "--output", "json"},
		},
		{
			name: "an explicit separator is not doubled",
			args: []string{"ext", "azure", "--", "--dry-run"},
			want: []string{"ext", "azure", "--", "--dry-run"},
		},
		{
			name: "help is left to cobra",
			args: []string{"ext", "--help"},
			want: []string{"ext", "--help"},
		},
		{
			name: "other commands are untouched",
			args: []string{"datasources", "list", "--output", "json"},
			want: []string{"datasources", "list", "--output", "json"},
		},
		{
			name: "bare ext is untouched",
			args: []string{"ext"},
			want: []string{"ext"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := root.RewriteExtensionArgs(root.Command("test"), tt.args)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}
