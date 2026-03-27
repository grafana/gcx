package main

import (
	"log"
	"os"

	"github.com/grafana/gcx/cmd/gcx/root"
	"github.com/spf13/cobra/doc"
)

func main() {
	outputDir := "./docs/reference/cli"
	if len(os.Args) > 1 {
		outputDir = os.Args[1]
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		log.Fatal(err)
	}

	cmd := root.Command("version")
	cmd.DisableAutoGenTag = true

	err := doc.GenMarkdownTree(cmd, outputDir)
	if err != nil {
		log.Fatal(err)
	}
}
