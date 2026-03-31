package helptree_test

import (
	"strings"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/helptree"
	"github.com/grafana/gcx/internal/agent"
	"github.com/spf13/cobra"
)

// buildTestTree creates a minimal cobra command tree for testing.
//
//	gcx — Test CLI
//	  foo [NAME] --output(-o)=(json|yaml) --limit=INT  # hint: --limit 10
//	  bar — Do bar things
//	    baz
//	    qux <KIND> --on-error=(ignore|fail|abort) --dry-run
//	  secret (hidden)
func buildTestTree() *cobra.Command {
	root := &cobra.Command{
		Use:   "gcx",
		Short: "Test CLI",
	}

	foo := &cobra.Command{
		Use:   "foo [NAME]",
		Short: "Do foo things",
		Annotations: map[string]string{
			agent.AnnotationLLMHint: "--limit 10",
		},
	}
	foo.Flags().StringP("output", "o", "json", "Output format. One of: json, yaml")
	foo.Flags().Int("limit", 100, "Max results to return")

	bar := &cobra.Command{
		Use:   "bar",
		Short: "Do bar things",
	}

	baz := &cobra.Command{
		Use:   "baz",
		Short: "Nested baz",
	}

	qux := &cobra.Command{
		Use:   "qux <KIND>",
		Short: "Do qux things",
	}
	qux.Flags().String("on-error", "fail", "Error handling. One of: ignore, fail, abort")
	qux.Flags().Bool("dry-run", false, "Dry run mode")

	bar.AddCommand(baz)
	bar.AddCommand(qux)

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

// --- Cycle 1: detectEnum ---

func TestDetectEnum(t *testing.T) {
	tests := []struct {
		usage string
		want  string
	}{
		{"Output format. One of: json, yaml, text", "(json|yaml|text)"},
		{"One of: json, yaml", "(json|yaml)"},
		{"Error handling. One of: ignore, fail, abort", "(ignore|fail|abort)"},
		{"Behavior on error. One of: dry-run, live-run, rollback", "(dry-run|live-run|rollback)"},
		{"Max results to return", ""},
		{"Enable debug mode", ""},
		{"", ""},
	}

	for _, tt := range tests {
		got := helptree.DetectEnum(tt.usage)
		if got != tt.want {
			t.Errorf("helptree.DetectEnum(%q) = %q, want %q", tt.usage, got, tt.want)
		}
	}
}

// --- Cycle 2: formatFlag ---

func TestFormatFlag(t *testing.T) {
	// String flag with shorthand and enum → -o=(json|yaml)
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().StringP("output", "o", "json", "Output format. One of: json, yaml")
	cmd.Flags().String("on-error", "fail", "Error handling. One of: ignore, fail, abort")
	cmd.Flags().IntP("count", "n", 0, "Number of items")
	cmd.Flags().Bool("dry-run", false, "Dry run mode")
	cmd.Flags().StringP("body", "b", "", "Request body")
	cmd.Flags().CountP("verbose", "v", "Verbose mode")

	tests := []struct {
		name string
		want string
	}{
		{"output", "-o=(json|yaml)"},
		{"on-error", "--on-error=(ignore|fail|abort)"},
		{"count", "-n=INT"},
		{"dry-run", "--dry-run"},
		{"body", "-b=STR"},
		{"verbose", "-v"},
	}

	for _, tt := range tests {
		f := cmd.Flags().Lookup(tt.name)
		if f == nil {
			t.Fatalf("flag %q not found", tt.name)
		}
		got := helptree.FormatFlag(f)
		if got != tt.want {
			t.Errorf("helptree.FormatFlag(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

// --- Cycle 3: Basic tree rendering ---

func TestRenderTree_Basic(t *testing.T) {
	root := buildTestTree()
	got := helptree.RenderTree(root, helptree.RenderOptions{})

	// Verify key structural elements.
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")

	// Root line
	if lines[0] != "gcx — Test CLI" {
		t.Errorf("root line = %q, want %q", lines[0], "gcx — Test CLI")
	}

	// foo is a leaf with args, flags, and hint
	fooLine := findLine(lines, "foo")
	if fooLine == "" {
		t.Fatal("foo line not found")
	}
	if !strings.HasPrefix(fooLine, "  foo") {
		t.Errorf("foo should be indented 2 spaces: %q", fooLine)
	}
	if !strings.Contains(fooLine, "[NAME]") {
		t.Errorf("foo should show args: %q", fooLine)
	}
	if !strings.Contains(fooLine, "-o=(json|yaml)") {
		t.Errorf("foo should show enum flag: %q", fooLine)
	}
	if !strings.Contains(fooLine, "# hint: --limit 10") {
		t.Errorf("foo should show hint: %q", fooLine)
	}

	// bar is a branch with description
	barLine := findLine(lines, "bar")
	if barLine == "" {
		t.Fatal("bar line not found")
	}
	if !strings.Contains(barLine, "— Do bar things") {
		t.Errorf("bar should show description: %q", barLine)
	}

	// baz is nested under bar (4 spaces)
	bazLine := findLine(lines, "baz")
	if bazLine == "" {
		t.Fatal("baz line not found")
	}
	if !strings.HasPrefix(bazLine, "    baz") {
		t.Errorf("baz should be indented 4 spaces: %q", bazLine)
	}

	// qux is a leaf with args and flags
	quxLine := findLine(lines, "qux")
	if quxLine == "" {
		t.Fatal("qux line not found")
	}
	if !strings.Contains(quxLine, "<KIND>") {
		t.Errorf("qux should show args: %q", quxLine)
	}
	if !strings.Contains(quxLine, "--on-error=(ignore|fail|abort)") {
		t.Errorf("qux should show enum flag: %q", quxLine)
	}
	if !strings.Contains(quxLine, "--dry-run") {
		t.Errorf("qux should show bool flag: %q", quxLine)
	}

	// hidden command should NOT appear
	secretLine := findLine(lines, "secret")
	if secretLine != "" {
		t.Errorf("hidden command should not appear: %q", secretLine)
	}
}

// --- Cycle 4: Depth limiting ---

func TestRenderTree_Depth(t *testing.T) {
	root := buildTestTree()

	t.Run("depth 1", func(t *testing.T) {
		got := helptree.RenderTree(root, helptree.RenderOptions{MaxDepth: 1})
		lines := strings.Split(strings.TrimRight(got, "\n"), "\n")

		// Should have root + direct children (foo, bar) = 3 lines
		// No grandchildren (baz, qux)
		if findLine(lines, "baz") != "" {
			t.Error("depth 1 should not show grandchildren (baz)")
		}
		if findLine(lines, "qux") != "" {
			t.Error("depth 1 should not show grandchildren (qux)")
		}
		if findLine(lines, "foo") == "" {
			t.Error("depth 1 should show direct children (foo)")
		}
		if findLine(lines, "bar") == "" {
			t.Error("depth 1 should show direct children (bar)")
		}
	})

	t.Run("depth 2", func(t *testing.T) {
		got := helptree.RenderTree(root, helptree.RenderOptions{MaxDepth: 2})
		lines := strings.Split(strings.TrimRight(got, "\n"), "\n")

		// Should include grandchildren
		if findLine(lines, "baz") == "" {
			t.Error("depth 2 should show grandchildren (baz)")
		}
	})

	t.Run("depth 0 unlimited", func(t *testing.T) {
		got := helptree.RenderTree(root, helptree.RenderOptions{MaxDepth: 0})
		lines := strings.Split(strings.TrimRight(got, "\n"), "\n")

		if findLine(lines, "baz") == "" {
			t.Error("depth 0 (unlimited) should show all nodes")
		}
	})
}

// --- Cycle 5: Subtree filtering ---

func TestFindSubtree(t *testing.T) {
	root := buildTestTree()

	t.Run("direct child", func(t *testing.T) {
		cmd := helptree.FindSubtree(root, "bar")
		if cmd == nil {
			t.Fatal("helptree.FindSubtree(bar) returned nil")
		}
		if cmd.Name() != "bar" {
			t.Errorf("helptree.FindSubtree(bar).Name() = %q", cmd.Name())
		}
	})

	t.Run("nested child", func(t *testing.T) {
		cmd := helptree.FindSubtree(root, "bar baz")
		if cmd == nil {
			t.Fatal("helptree.FindSubtree(bar baz) returned nil")
		}
		if cmd.Name() != "baz" {
			t.Errorf("helptree.FindSubtree(bar baz).Name() = %q", cmd.Name())
		}
	})

	t.Run("nonexistent", func(t *testing.T) {
		cmd := helptree.FindSubtree(root, "nonexistent")
		if cmd != nil {
			t.Error("helptree.FindSubtree(nonexistent) should return nil")
		}
	})
}

// --- Cycle 6: Hints ---

func TestRenderTree_Hints(t *testing.T) {
	root := buildTestTree()
	got := helptree.RenderTree(root, helptree.RenderOptions{})
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")

	// foo has a hint
	fooLine := findLine(lines, "foo")
	if !strings.Contains(fooLine, "# hint: --limit 10") {
		t.Errorf("foo should have hint: %q", fooLine)
	}

	// baz has no hint
	bazLine := findLine(lines, "baz")
	if strings.Contains(bazLine, "# hint") {
		t.Errorf("baz should not have hint: %q", bazLine)
	}
}

// --- Cycle 7: Command wiring ---

func TestCommand_Execute(t *testing.T) {
	root := buildTestTree()
	cmd := helptree.Command(root)

	buf := &strings.Builder{}
	cmd.SetOut(buf)
	cmd.SetErr(&strings.Builder{})
	cmd.SetArgs([]string{"--output", "text"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "gcx — Test CLI") {
		t.Errorf("output should contain root: %q", output[:min(100, len(output))])
	}
}

func TestCommand_DepthFlag(t *testing.T) {
	root := buildTestTree()
	cmd := helptree.Command(root)

	buf := &strings.Builder{}
	cmd.SetOut(buf)
	cmd.SetErr(&strings.Builder{})
	cmd.SetArgs([]string{"--depth", "1"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if findLine(lines, "baz") != "" {
		t.Error("--depth 1 should not show grandchildren")
	}
}

func TestCommand_SubtreeArg(t *testing.T) {
	root := buildTestTree()
	cmd := helptree.Command(root)

	buf := &strings.Builder{}
	cmd.SetOut(buf)
	cmd.SetErr(&strings.Builder{})
	cmd.SetArgs([]string{"--output", "text", "bar"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	output := buf.String()
	if !strings.HasPrefix(output, "bar") {
		t.Errorf("subtree output should start with bar: %q", output[:min(50, len(output))])
	}
	if strings.Contains(output, "foo") {
		t.Error("subtree bar should not contain foo")
	}
}

func TestCommand_MultiSegmentSubtree(t *testing.T) {
	root := buildTestTree()
	cmd := helptree.Command(root)

	buf := &strings.Builder{}
	cmd.SetOut(buf)
	cmd.SetErr(&strings.Builder{})
	cmd.SetArgs([]string{"--output", "text", "bar", "baz"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	output := buf.String()
	if !strings.HasPrefix(output, "baz") {
		t.Errorf("multi-segment subtree output should start with baz: %q", output[:min(50, len(output))])
	}
}

func TestCommand_SubtreeNotFound(t *testing.T) {
	root := buildTestTree()
	cmd := helptree.Command(root)

	buf := &strings.Builder{}
	cmd.SetOut(buf)
	cmd.SetErr(&strings.Builder{})
	cmd.SetArgs([]string{"nonexistent"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for nonexistent subtree")
	}
}

func TestCommand_JSONOutput(t *testing.T) {
	root := buildTestTree()
	cmd := helptree.Command(root)

	buf := &strings.Builder{}
	cmd.SetOut(buf)
	cmd.SetErr(&strings.Builder{})
	cmd.SetArgs([]string{"--output", "json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, `"name"`) {
		t.Errorf("JSON output should contain structured fields: %q", output[:min(100, len(output))])
	}
	if !strings.Contains(output, `"children"`) {
		t.Errorf("JSON output should contain children: %q", output[:min(200, len(output))])
	}
}

// findLine returns the first line containing the given command name as a word boundary.
func findLine(lines []string, name string) string {
	for _, line := range lines {
		trimmed := strings.TrimLeft(line, " ")
		if strings.HasPrefix(trimmed, name+" ") || strings.HasPrefix(trimmed, name+"\n") || trimmed == name {
			return line
		}
	}
	return ""
}
