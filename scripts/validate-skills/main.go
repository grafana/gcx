package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type skillFrontMatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// Both trees in this repo whose SKILL.md front matter has to parse: the
// portable bundle that ships inside the binary, and the repo-local contributor
// skills. Both are loaded by agent harnesses, so broken front matter silently
// drops a skill from either one.
const (
	bundleRoot    = "claude-plugin/skills"
	repoLocalRoot = ".claude/skills"
)

func main() {
	bundled, err := skillFilesUnder(bundleRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "walk %s: %v\n", bundleRoot, err)
		os.Exit(1)
	}
	if len(bundled) == 0 {
		fmt.Fprintf(os.Stderr, "no SKILL.md files found under %s\n", bundleRoot)
		os.Exit(1)
	}

	// Only git-tracked files in the repo-local tree: it is also where a
	// contributor's own harness installs third-party skills (they are not
	// gitignored), and a skill this repo does not own must not be able to fail
	// this repo's build with a defect no committed file can fix.
	repoLocal, err := trackedSkillFilesUnder(repoLocalRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "list tracked files under %s: %v\n", repoLocalRoot, err)
		os.Exit(1)
	}
	if len(repoLocal) == 0 {
		fmt.Fprintf(os.Stderr, "no tracked SKILL.md files found under %s\n", repoLocalRoot)
		os.Exit(1)
	}

	var failures []string
	for _, path := range append(bundled, repoLocal...) {
		if err := validateSkill(path); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", path, err))
		}
	}

	if len(failures) > 0 {
		for _, failure := range failures {
			fmt.Fprintln(os.Stderr, failure)
		}
		os.Exit(1)
	}

	_, _ = fmt.Fprintf(os.Stdout, "validated %s, %s\n", bundleRoot, repoLocalRoot)
}

// skillFilesUnder returns every SKILL.md below root.
func skillFilesUnder(root string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && d.Name() == "SKILL.md" {
			paths = append(paths, path)
		}
		return nil
	})
	return paths, err
}

// trackedSkillFilesUnder returns every git-tracked SKILL.md below root.
func trackedSkillFilesUnder(root string) ([]string, error) {
	out, err := exec.CommandContext(context.Background(), "git", "ls-files", "-z", "--", root).Output()
	if err != nil {
		return nil, err
	}

	var paths []string
	for rel := range strings.SplitSeq(strings.TrimRight(string(out), "\x00"), "\x00") {
		if rel != "" && filepath.Base(rel) == "SKILL.md" {
			paths = append(paths, rel)
		}
	}
	return paths, nil
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
