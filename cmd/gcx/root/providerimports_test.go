package root_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// registerCall matches a provider self-registration call in a provider package.
var registerCall = regexp.MustCompile(`\bproviders\.Register\(`)

// TestProviderPackagesAreBlankImported fails when a package under
// internal/providers/ registers a provider but command.go does not blank-import
// it. Without the import the provider compiles and its own tests pass, but it
// is missing from the binary and from every test that walks the root command
// tree.
func TestProviderPackagesAreBlankImported(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "command.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse command.go: %v", err)
	}
	imported := map[string]bool{}
	for _, spec := range file.Imports {
		if spec.Name == nil || spec.Name.Name != "_" {
			continue
		}
		p, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Fatalf("unquote import %s: %v", spec.Path.Value, err)
		}
		imported[p] = true
	}

	providersDir := filepath.Join("..", "..", "..", "internal", "providers")
	entries, err := os.ReadDir(providersDir)
	if err != nil {
		t.Fatalf("read %s: %v", providersDir, err)
	}

	registering := 0
	for _, entry := range entries {
		if !entry.IsDir() || !registersProvider(t, filepath.Join(providersDir, entry.Name())) {
			continue
		}
		registering++
		importPath := "github.com/grafana/gcx/internal/providers/" + entry.Name()
		if !imported[importPath] {
			t.Errorf("internal/providers/%s calls providers.Register but cmd/gcx/root/command.go does not blank-import %q", entry.Name(), importPath)
		}
	}
	if registering == 0 {
		t.Fatal("found no provider packages calling providers.Register; the scan is broken")
	}
}

// registersProvider reports whether a non-test Go file directly in dir calls
// providers.Register.
func registersProvider(t *testing.T, dir string) bool {
	t.Helper()
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	for _, f := range files {
		name := f.Name()
		if f.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if registerCall.Match(body) {
			return true
		}
	}
	return false
}
