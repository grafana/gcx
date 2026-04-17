package commands_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/commands"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// buildTestTree creates a minimal cobra command tree for testing.
func buildTestTree() *cobra.Command {
	root := &cobra.Command{
		Use:   "gcx",
		Short: "Test CLI",
	}

	foo := &cobra.Command{
		Use:     "foo [NAME]",
		Short:   "Do foo things",
		Long:    "Detailed description of foo.",
		Example: "  gcx foo myname",
		Annotations: map[string]string{
			agent.AnnotationTokenCost:      "small",
			agent.AnnotationLLMHint:        "--limit 10",
			agent.AnnotationRequiredAction: "foo:read",
		},
	}
	foo.Flags().StringP("output", "o", "json", "Output format")
	foo.Flags().Int("limit", 100, "Max results")

	bar := &cobra.Command{
		Use:   "bar",
		Short: "Do bar things",
	}

	baz := &cobra.Command{
		Use:   "baz",
		Short: "Nested baz",
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "large",
		},
	}
	bar.AddCommand(baz)

	hidden := &cobra.Command{
		Use:    "secret",
		Short:  "Hidden command",
		Hidden: true,
	}

	root.AddCommand(foo)
	root.AddCommand(bar)
	root.AddCommand(hidden)

	return root
}

func TestWalkCommand(t *testing.T) {
	root := buildTestTree()
	info := commands.WalkCommand(root, "")

	if info.FullPath != "gcx" {
		t.Errorf("expected full_path 'gcx', got %q", info.FullPath)
	}
	if info.Description != "Test CLI" {
		t.Errorf("expected description 'Test CLI', got %q", info.Description)
	}

	// Should have 2 visible subcommands (foo, bar), not hidden
	if len(info.Subcommands) != 2 {
		t.Fatalf("expected 2 subcommands, got %d", len(info.Subcommands))
	}

	// Find foo
	var foo *commands.CommandInfo
	for i := range info.Subcommands {
		if info.Subcommands[i].FullPath == "gcx foo" {
			foo = &info.Subcommands[i]
			break
		}
	}
	if foo == nil {
		t.Fatal("foo subcommand not found")
	}

	if foo.Description != "Do foo things" {
		t.Errorf("foo description = %q", foo.Description)
	}
	if foo.Long != "Detailed description of foo." {
		t.Errorf("foo long = %q", foo.Long)
	}
	if foo.Example != "  gcx foo myname" {
		t.Errorf("foo example = %q", foo.Example)
	}
	if foo.TokenCost != "small" {
		t.Errorf("foo token_cost = %q", foo.TokenCost)
	}
	if foo.LLMHint != "--limit 10" {
		t.Errorf("foo llm_hint = %q", foo.LLMHint)
	}
	if foo.RequiredAction != "foo:read" {
		t.Errorf("foo required_action = %q", foo.RequiredAction)
	}
	if foo.Args != "[NAME]" {
		t.Errorf("foo args = %q", foo.Args)
	}

	// Check flags
	if len(foo.Flags) != 2 {
		t.Fatalf("expected 2 flags on foo, got %d", len(foo.Flags))
	}

	flagsByName := map[string]commands.FlagInfo{}
	for _, f := range foo.Flags {
		flagsByName[f.Name] = f
	}

	outputFlag, ok := flagsByName["output"]
	if !ok {
		t.Fatal("output flag not found")
	}
	if outputFlag.Shorthand != "o" {
		t.Errorf("output shorthand = %q", outputFlag.Shorthand)
	}
	if outputFlag.Type != "string" {
		t.Errorf("output type = %q", outputFlag.Type)
	}
	if outputFlag.Default != "json" {
		t.Errorf("output default = %q", outputFlag.Default)
	}

	limitFlag, ok := flagsByName["limit"]
	if !ok {
		t.Fatal("limit flag not found")
	}
	if limitFlag.Type != "int" {
		t.Errorf("limit type = %q", limitFlag.Type)
	}
	if limitFlag.Default != "100" {
		t.Errorf("limit default = %q", limitFlag.Default)
	}
}

func TestWalkCommandNested(t *testing.T) {
	root := buildTestTree()
	info := commands.WalkCommand(root, "")

	// Find bar
	var bar *commands.CommandInfo
	for i := range info.Subcommands {
		if info.Subcommands[i].FullPath == "gcx bar" {
			bar = &info.Subcommands[i]
			break
		}
	}
	if bar == nil {
		t.Fatal("bar subcommand not found")
	}

	if len(bar.Subcommands) != 1 {
		t.Fatalf("expected 1 subcommand under bar, got %d", len(bar.Subcommands))
	}

	baz := bar.Subcommands[0]
	if baz.FullPath != "gcx bar baz" {
		t.Errorf("baz full_path = %q", baz.FullPath)
	}
	if baz.TokenCost != "large" {
		t.Errorf("baz token_cost = %q", baz.TokenCost)
	}
}

func TestWalkCommandIncludeHidden(t *testing.T) {
	root := buildTestTree()
	info := commands.WalkCommandWithOptions(root, "", true)

	// Should have 3 subcommands including hidden
	if len(info.Subcommands) != 3 {
		t.Fatalf("expected 3 subcommands with hidden, got %d", len(info.Subcommands))
	}

	var found bool
	for _, sub := range info.Subcommands {
		if sub.FullPath == "gcx secret" {
			found = true
			break
		}
	}
	if !found {
		t.Error("hidden command 'secret' not found when includeHidden=true")
	}
}

func TestFlattenCommands(t *testing.T) {
	root := buildTestTree()
	tree := commands.WalkCommand(root, "")
	flat := commands.FlattenCommands(tree)

	// gcx, foo, bar, baz = 4 total
	if len(flat) != 4 {
		t.Fatalf("expected 4 flat commands, got %d", len(flat))
	}

	// All should have empty subcommands
	for _, cmd := range flat {
		if len(cmd.Subcommands) != 0 {
			t.Errorf("flat command %q has non-empty subcommands", cmd.FullPath)
		}
	}

	// Check that all paths are present
	paths := map[string]bool{}
	for _, cmd := range flat {
		paths[cmd.FullPath] = true
	}
	for _, expected := range []string{"gcx", "gcx foo", "gcx bar", "gcx bar baz"} {
		if !paths[expected] {
			t.Errorf("missing path %q in flat output", expected)
		}
	}
}

func TestCollectResourceTypes(t *testing.T) {
	wellKnown := []agent.KnownResource{
		{
			Kind:    "Dashboard",
			Group:   "dashboard.grafana.app",
			Version: "v1beta1",
			Aliases: []string{"dashboards", "dash"},
			Operations: map[string]agent.OperationHint{
				"get":  {TokenCost: "large", LLMHint: "gcx resources get dashboards/my-uid -o json"},
				"push": {TokenCost: "small", LLMHint: "gcx resources push -p ./dashboards"},
			},
		},
	}

	adapterRegs := []adapter.Registration{
		{
			Descriptor: resources.Descriptor{
				GroupVersion: schema.GroupVersion{Group: "slo.grafana.app", Version: "v1"},
			},
			Aliases: []string{"slo", "slos"},
			GVK:     schema.GroupVersionKind{Group: "slo.grafana.app", Version: "v1", Kind: "SLODefinition"},
			Operations: map[string]agent.OperationHint{
				"get": {TokenCost: "medium", LLMHint: "gcx slo definitions list --limit 50 -o json"},
			},
		},
	}

	types := commands.ExportCollectResourceTypes(wellKnown, adapterRegs)

	if len(types) != 2 {
		t.Fatalf("expected 2 resource types, got %d", len(types))
	}

	typesByKind := map[string]commands.ResourceTypeInfo{}
	for _, rt := range types {
		typesByKind[rt.Kind] = rt
	}

	dash, ok := typesByKind["Dashboard"]
	if !ok {
		t.Fatal("Dashboard not found")
	}
	if dash.Source != "well-known" {
		t.Errorf("Dashboard source = %q", dash.Source)
	}
	if getOp, exists := dash.Operations["get"]; !exists || getOp.TokenCost != "large" {
		t.Errorf("Dashboard get operation token_cost = %v", dash.Operations["get"])
	}
	if pushOp, exists := dash.Operations["push"]; !exists || pushOp.TokenCost != "small" {
		t.Errorf("Dashboard push operation token_cost = %v", dash.Operations["push"])
	}

	slo, ok := typesByKind["SLODefinition"]
	if !ok {
		t.Fatal("SLODefinition not found")
	}
	if slo.Source != "adapter" {
		t.Errorf("SLO source = %q", slo.Source)
	}
	if getOp, exists := slo.Operations["get"]; !exists || getOp.TokenCost != "medium" {
		t.Errorf("SLO get operation token_cost = %v", slo.Operations["get"])
	}
}

func TestCommandJSONOutput(t *testing.T) {
	root := buildTestTree()
	cmd := commands.NewTestCommand(root)

	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	var output commands.CatalogOutput
	if err := json.Unmarshal(buf.Bytes(), &output); err != nil {
		t.Fatalf("json unmarshal failed: %v\nraw: %s", err, buf.String())
	}

	if output.Commands.FullPath != "gcx" {
		t.Errorf("commands.full_path = %q", output.Commands.FullPath)
	}

	if output.ResourceTypes == nil {
		t.Error("resource_types is nil")
	}
}

func TestCommandFlatOutput(t *testing.T) {
	root := buildTestTree()
	cmd := commands.NewTestCommand(root)

	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--flat"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	var output commands.FlatCatalogOutput
	if err := json.Unmarshal(buf.Bytes(), &output); err != nil {
		t.Fatalf("json unmarshal failed: %v\nraw: %s", err, buf.String())
	}

	if len(output.Commands) != 4 {
		t.Errorf("expected 4 flat commands, got %d", len(output.Commands))
	}
}

func TestArgsExtraction(t *testing.T) {
	tests := []struct {
		use  string
		want string
	}{
		{"foo [NAME]", "[NAME]"},
		{"foo", ""},
		{"push [SELECTOR]...", "[SELECTOR]..."},
		{"set PROPERTY_NAME PROPERTY_VALUE", "PROPERTY_NAME PROPERTY_VALUE"},
		{"get [SELECTOR]...", "[SELECTOR]..."},
	}

	for _, tt := range tests {
		got := commands.ExtractArgs(tt.use)
		if got != tt.want {
			t.Errorf("extractArgs(%q) = %q, want %q", tt.use, got, tt.want)
		}
	}
}
