package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/grafana/gcx/internal/linter"
)

func main() {
	outputDir := "./docs/reference/linter-rules"
	if len(os.Args) > 1 {
		outputDir = os.Args[1]
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		log.Fatal(err)
	}

	engine, err := linter.New()
	if err != nil {
		log.Fatal(err)
	}

	rules, err := engine.Rules(context.Background())
	if err != nil {
		log.Fatal(err)
	}

	err = os.WriteFile(filepath.Join(outputDir, "index.md"), toMarkdown(rules), 0600)
	if err != nil {
		log.Fatal(err)
	}
}

func toMarkdown(rules []linter.Rule) []byte {
	buffer := bytes.Buffer{}

	buffer.WriteString("# Linter rules reference\n\n")

	resourceTypesMap := map[string]struct{}{}
	categoriesMap := map[string]struct{}{}
	// groupedRules[resource][category]
	groupedRules := map[string]map[string][]linter.Rule{}
	for _, r := range rules {
		resourceTypesMap[r.Resource] = struct{}{}
		categoriesMap[r.Category] = struct{}{}

		if _, ok := groupedRules[r.Resource]; !ok {
			groupedRules[r.Resource] = map[string][]linter.Rule{}
		}

		groupedRules[r.Resource][r.Category] = append(groupedRules[r.Resource][r.Category], r)
	}

	resourceTypes := slices.Collect(maps.Keys(resourceTypesMap))
	sort.Strings(resourceTypes)

	categories := slices.Collect(maps.Keys(categoriesMap))
	sort.Strings(categories)

	for _, resource := range resourceTypes {
		fmt.Fprintf(&buffer, "## `%s`\n\n", resource)

		buffer.WriteString("| Category | Severity | Name | Summary |\n")
		buffer.WriteString("| -------- | -------- | ---- | ------- |\n")

		for _, category := range categories {
			rules := groupedRules[resource][category]
			slices.SortStableFunc(rules, func(a linter.Rule, b linter.Rule) int {
				return strings.Compare(a.Name, b.Name)
			})

			for _, rule := range rules {
				ruleName := fmt.Sprintf("`%s`", rule.Name)
				if rule.DocumentationURL() != "" {
					prefix := "https://github.com/grafana/gcx/blob/main/docs/reference/linter-rules/"
					url := strings.TrimPrefix(rule.DocumentationURL(), prefix)

					if url != rule.DocumentationURL() {
						url = "./" + url
					}

					ruleName = fmt.Sprintf("[%s](%s)", ruleName, url)
				}

				fmt.Fprintf(&buffer, "| `%s` | `%s` | %s | %s |\n", rule.Category, rule.Severity, ruleName, rule.Description)
			}
		}

		buffer.WriteString("\n")
	}

	buffer.WriteString("\n")

	return buffer.Bytes()
}
