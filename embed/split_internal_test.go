package embed

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSplitCommand(t *testing.T) {
	tests := []struct {
		command string
		want    []string
	}{
		{"", nil},
		{"gcx", []string{}},
		{"gcx slo list", []string{"slo", "list"}},
		{"slo list", []string{"slo", "list"}},
		{"  metrics   query\t'up{job=\"api\"}'  ", []string{"metrics", "query", `up{job="api"}`}},
		{`logs query "{app=\"x\"} |= \"error\""`, []string{"logs", "query", `{app="x"} |= "error"`}},
		{`resources get dashboards/my\ dash`, []string{"resources", "get", "dashboards/my dash"}},
		{`api /api/search -q 'a b'c`, []string{"api", "/api/search", "-q", "a bc"}},
		{`x '' ""`, []string{"x", "", ""}},
		{`traces query '$foo | bar'`, []string{"traces", "query", "$foo | bar"}},
	}
	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			got, err := splitCommand(tt.command)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSplitCommandRejectsShellSyntax(t *testing.T) {
	for _, command := range []string{
		"slo list | jq .",
		"slo list > out.json",
		"slo list; rm -rf /",
		"slo list && echo",
		"slo get $(whoami)",
		"slo get `whoami`",
		`slo get "$HOME"`,
		"slo get $HOME",
		"slo list &",
	} {
		t.Run(command, func(t *testing.T) {
			_, err := splitCommand(command)
			require.ErrorIs(t, err, errShell)
		})
	}

	for _, command := range []string{`slo get 'oops`, `slo get "oops`, `slo get oops\`} {
		t.Run(command, func(t *testing.T) {
			_, err := splitCommand(command)
			require.Error(t, err)
		})
	}
}
