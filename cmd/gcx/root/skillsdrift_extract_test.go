package root_test

import (
	"maps"
	"reflect"
	"slices"
	"testing"
	"testing/fstest"
)

// TestSkillDocsFromFS pins that the skill walk reaches nested reference
// markdown, not just the SKILL.md at each root.
//
// This is the assertion that used to be an invocation-count floor. A floor
// guarded the same regression only by accident — it noticed that the total had
// dropped — while also snapshotting how much the skills happened to say, so it
// rotted on every rewrite and blocked consolidation. A synthetic tree tests the
// traversal directly and stays true no matter how the real skills change.
func TestSkillDocsFromFS(t *testing.T) {
	fsys := fstest.MapFS{
		"alpha/SKILL.md":                  {Data: []byte("# alpha\n")},
		"alpha/references/deep.md":        {Data: []byte("# deep\n")},
		"alpha/references/nested/more.md": {Data: []byte("# more\n")},
		"beta/SKILL.md":                   {Data: []byte("# beta\n")},
		"beta/assets/template.txt":        {Data: []byte("not markdown\n")},
		"beta/scripts/run.sh":             {Data: []byte("#!/bin/sh\n")},
		"README.md":                       {Data: []byte("# top level\n")},
	}

	docs, err := skillDocsFromFS(fsys, "tree")
	if err != nil {
		t.Fatalf("skillDocsFromFS() error = %v", err)
	}

	want := []string{
		"tree/README.md",
		"tree/alpha/SKILL.md",
		"tree/alpha/references/deep.md",
		"tree/alpha/references/nested/more.md",
		"tree/beta/SKILL.md",
	}
	if got := slices.Sorted(maps.Keys(docs)); !reflect.DeepEqual(got, want) {
		t.Errorf("skillDocsFromFS() keys = %v, want %v", got, want)
	}

	if got := docs["tree/alpha/references/nested/more.md"]; got != "# more\n" {
		t.Errorf("nested reference content = %q, want %q", got, "# more\n")
	}
}

// TestExtractInvocations pins the extraction behaviour of the skills drift
// check: which parts of a skill markdown document count as gcx invocations
// (shell fences, inline code spans), which spellings of the binary count
// (`gcx`, `bin/gcx`, `./bin/gcx` — contributor-facing skills invoke the binary
// they just built), and how shell syntax within them is tokenized
// (continuations, pipes, substitutions, quoting, placeholders).
// When TestSkillsGcxInvocationsMatchCommandTree misbehaves, these cases
// separate extractor bugs from genuine command-tree drift.
//
// The two extraction paths share the gcxCommandWords list but match it
// differently — inlineGcxCommands prefix-matches the whole code span, gcxArgs
// compares the first token after stripping env assignments and shell keywords.
// A spelling therefore has to survive two different matchers, and only an
// extractInvocations-level test exercises both. Fence and inline cases below are
// deliberately paired for that reason.
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
			name:    "built binary in a shell fence",
			content: "```bash\nbin/gcx help-tree\n```\n",
			want:    []invocation{{line: 2, args: []string{"help-tree"}}},
		},
		{
			name:    "built binary with leading ./ in a shell fence",
			content: "```sh\n./bin/gcx providers list\n```\n",
			want:    []invocation{{line: 2, args: []string{"providers", "list"}}},
		},
		{
			name:    "built binary in an inline span",
			content: "read it back with `bin/gcx commands --flat` first\n",
			want:    []invocation{{line: 1, args: []string{"commands", "--flat"}}},
		},
		{
			name:    "env prefix before the built binary",
			content: "```bash\nGCX_AGENT_MODE=false bin/gcx help-tree\n```\n",
			want:    []invocation{{line: 2, args: []string{"help-tree"}}},
		},
		{
			name:    "unrelated path ending in gcx is not an invocation",
			content: "```bash\nother/gcx help-tree\n```\n",
			want:    nil,
		},
		{
			name:    "built binary inside a non-shell fence not scanned",
			content: "```text\nbin/gcx not-a-real-command\n```\n",
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
