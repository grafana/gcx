package scripts_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestPackslipResourcesCoverBundledSkills(t *testing.T) {
	workflowData, err := os.ReadFile(filepath.Join("..", ".github", "workflows", "release.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Jobs map[string]struct {
			Steps []struct {
				Name string            `yaml:"name"`
				With map[string]string `yaml:"with"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(workflowData, &workflow); err != nil {
		t.Fatalf("parse release workflow: %v", err)
	}

	var resources string
	resourceSteps := 0
	for _, job := range workflow.Jobs {
		for _, step := range job.Steps {
			if step.Name == "Publish signed Packslip manifest" {
				resourceSteps++
				resources = step.With["resources"]
			}
		}
	}
	if resourceSteps != 1 {
		t.Fatalf("expected one Packslip publishing step, found %d", resourceSteps)
	}

	want := map[string]bool{}
	skillRoot := filepath.Join("..", "claude-plugin", "skills")
	entries, err := os.ReadDir(skillRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(skillRoot, entry.Name(), "SKILL.md")); err == nil {
			want["skill/"+entry.Name()+"=repo:claude-plugin/skills/"+entry.Name()] = true
		} else if !os.IsNotExist(err) {
			t.Fatalf("check bundled skill %q: %v", entry.Name(), err)
		}
	}

	got := map[string]bool{}
	for line := range strings.SplitSeq(resources, "\n") {
		resource := strings.TrimSpace(line)
		if resource == "" {
			continue
		}
		if got[resource] {
			t.Errorf("duplicate Packslip resource %q", resource)
		}
		got[resource] = true
	}
	for resource := range want {
		if !got[resource] {
			t.Errorf("bundled skill is missing from Packslip resources: %s", resource)
		}
	}
	for resource := range got {
		if !want[resource] {
			t.Errorf("Packslip resource does not match a bundled skill: %s", resource)
		}
	}
}
