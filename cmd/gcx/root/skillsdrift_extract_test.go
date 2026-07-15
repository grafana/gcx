package root_test

import (
	"reflect"
	"testing"
)

// TestExtractInvocations pins the extraction behaviour of the skills drift
// check: which parts of a skill markdown document count as gcx invocations
// (shell fences, inline code spans) and how shell syntax within them is
// tokenized (continuations, pipes, substitutions, quoting, placeholders).
// When TestSkillsGcxInvocationsMatchCommandTree misbehaves, these cases
// separate extractor bugs from genuine command-tree drift.
func TestExtractInvocations(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []invocation
	}{
		{
			name:    "simple bash fence",
			content: "```bash\ngcx slo definitions list\n```\n",
			want:    []invocation{{line: 2, args: []string{"slo", "definitions", "list"}}},
		},
		{
			name:    "bare fence",
			content: "```\ngcx providers\n```\n",
			want:    []invocation{{line: 2, args: []string{"providers"}}},
		},
		{
			name:    "non-shell fence skipped",
			content: "```go\ngcx not a command\n```\n```promql\ngcx neither\n```\n",
			want:    nil,
		},
		{
			name:    "inline span in prose extracted",
			content: "run `gcx slo definitions list` to see them\n",
			want:    []invocation{{line: 1, args: []string{"slo", "definitions", "list"}}},
		},
		{
			name:    "inline span in table cell",
			content: "| delete | `gcx dashboards delete <name> --force` | notes |\n",
			want:    []invocation{{line: 1, args: []string{"dashboards", "delete", "<name>", "--force"}}},
		},
		{
			name:    "inline span with placeholder",
			content: "fetch it with `gcx slo definitions get <uid>` first\n",
			want:    []invocation{{line: 1, args: []string{"slo", "definitions", "get", "<uid>"}}},
		},
		{
			name:    "non-gcx inline span ignored",
			content: "use `kubectl get pods` instead\n",
			want:    nil,
		},
		{
			name:    "fragment inline spans ignored",
			content: "pass `--force` to `resources delete`, or just `gcx` alone\n",
			want:    nil,
		},
		{
			name:    "double-backtick span and second span",
			content: "``gcx providers`` and `gcx config check`\n",
			want: []invocation{
				{line: 1, args: []string{"providers"}},
				{line: 1, args: []string{"config", "check"}},
			},
		},
		{
			name:    "inline span inside non-shell fence not scanned",
			content: "```go\n// see `gcx providers` for details\n```\n",
			want:    nil,
		},
		{
			name:    "comment lines skipped",
			content: "```bash\n# gcx old command\ngcx synth checks list  # trailing comment\n```\n",
			want:    []invocation{{line: 3, args: []string{"synth", "checks", "list"}}},
		},
		{
			name:    "backslash continuation",
			content: "```bash\ngcx metrics query -d <uid> \\\n  'up{job=\"x\"}' \\\n  --from 1h\n```\n",
			want:    []invocation{{line: 2, args: []string{"metrics", "query", "-d", "<uid>", `up{job="x"}`, "--from", "1h"}}},
		},
		{
			name:    "env prefix and pipe",
			content: "```bash\nGCX_AGENT_MODE=true gcx dashboards list -o json | jq '.[]'\n```\n",
			want:    []invocation{{line: 2, args: []string{"dashboards", "list", "-o", "json"}}},
		},
		{
			name:    "command substitution in assignment",
			content: "```bash\nUID=$(gcx datasources list -t prometheus -o json 2>/dev/null | jq -r '.uid')\n```\n",
			want:    []invocation{{line: 2, args: []string{"datasources", "list", "-t", "prometheus", "-o", "json"}}},
		},
		{
			name:    "nested substitution as argument",
			content: "```bash\ngcx metrics query -d $(gcx datasources list -o json) 'up'\n```\n",
			want: []invocation{
				{line: 2, args: []string{"datasources", "list", "-o", "json"}},
				{line: 2, args: []string{"metrics", "query", "-d", "$(...)", "up"}},
			},
		},
		{
			name:    "chained with && and semicolon",
			content: "```bash\ngcx config check && gcx providers; gcx slo definitions list\n```\n",
			want: []invocation{
				{line: 2, args: []string{"config", "check"}},
				{line: 2, args: []string{"providers"}},
				{line: 2, args: []string{"slo", "definitions", "list"}},
			},
		},
		{
			name:    "placeholder not read as redirect",
			content: "```bash\ngcx resources get <kind> <name|uuid> -o yaml > out.yaml\n```\n",
			want:    []invocation{{line: 2, args: []string{"resources", "get", "<kind>", "<name|uuid>", "-o", "yaml"}}},
		},
		{
			name:    "loop keyword stripped",
			content: "```bash\nfor id in 1 2; do gcx synth checks get $id; done\n```\n",
			want:    []invocation{{line: 2, args: []string{"synth", "checks", "get", "$id"}}},
		},
		{
			name:    "double quoted argument kept whole",
			content: "```bash\ngcx logs query -d <uid> \"{job=\\\"app\\\"} |= `error`\"\n```\n",
			want:    []invocation{{line: 2, args: []string{"logs", "query", "-d", "<uid>", "{job=\"app\"} |= `error`"}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractInvocations(tt.content)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("extractInvocations() = %v, want %v", got, tt.want)
			}
		})
	}
}
