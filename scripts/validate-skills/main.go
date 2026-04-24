package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type skillFrontMatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

func main() {
	const root = "claude-plugin/skills"

	var failures []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", path, err))
			return nil
		}
		if d.IsDir() || d.Name() != "SKILL.md" {
			return nil
		}
		if err := validateSkill(path); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", path, err))
		}
		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "walk %s: %v\n", root, err)
		os.Exit(1)
	}

	if len(failures) > 0 {
		for _, failure := range failures {
			fmt.Fprintln(os.Stderr, failure)
		}
		os.Exit(1)
	}

	fmt.Printf("validated %s\n", root)
}

func validateSkill(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	frontMatter, err := extractFrontMatter(data)
	if err != nil {
		return err
	}

	var meta skillFrontMatter
	if err := yaml.Unmarshal([]byte(frontMatter), &meta); err != nil {
		return fmt.Errorf("invalid YAML front matter: %w", err)
	}
	if strings.TrimSpace(meta.Name) == "" {
		return errors.New("missing front matter field: name")
	}
	if strings.TrimSpace(meta.Description) == "" {
		return errors.New("missing front matter field: description")
	}

	return nil
}

func extractFrontMatter(data []byte) (string, error) {
	content := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(content, "\n")
	if len(lines) < 3 || strings.TrimSpace(lines[0]) != "---" {
		return "", errors.New("missing front matter")
	}

	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return "", errors.New("unterminated front matter")
	}

	return strings.Join(lines[1:end], "\n"), nil
}
