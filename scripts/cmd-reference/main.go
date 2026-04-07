package main

import (
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/grafana/gcx/cmd/gcx/root"
	"github.com/spf13/cobra/doc"
)

// dateDefault matches flag defaults that contain a YYYY-MM-DD date,
// e.g. (default "2026-03-30"), and replaces the date with a stable
// placeholder so that `make docs` output doesn't churn on every run.
var dateDefault = regexp.MustCompile(`\(default "(\d{4}-\d{2}-\d{2})"\)`)

func main() {
	outputDir := "./docs/reference/cli"
	if len(os.Args) > 1 {
		outputDir = os.Args[1]
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		log.Fatal(err)
	}

	// Use a stable executable name for generated docs. `go run` executes this
	// generator as a temporary binary named "main", which would otherwise leak
	// into Cobra-generated help/completion examples.
	os.Args[0] = "gcx"

	cmd := root.Command("version")
	cmd.DisableAutoGenTag = true

	if err := doc.GenMarkdownTree(cmd, outputDir); err != nil {
		log.Fatal(err)
	}

	// Post-process: stabilize date defaults in generated docs.
	if err := stabilizeDateDefaults(outputDir); err != nil {
		log.Fatal(err)
	}
}

func stabilizeDateDefaults(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}

		path := filepath.Join(dir, e.Name())

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		replaced := dateDefault.ReplaceAll(data, []byte(`(default "YYYY-MM-DD")`))
		if string(replaced) != string(data) {
			if err := os.WriteFile(path, replaced, 0600); err != nil {
				return err
			}
		}
	}

	return nil
}
