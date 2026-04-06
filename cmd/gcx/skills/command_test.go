package skills

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListCommand_EmbeddedSkills(t *testing.T) {
	cmd := Command()
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute list: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "gcx") {
		t.Fatalf("expected embedded gcx skill in output: %q", got)
	}
}

func TestListCommand_RespectsSourceOverride(t *testing.T) {
	sourceDir := t.TempDir()
	writeSkill(t, sourceDir, "team-custom", "team-custom", "Custom team skill")
	t.Setenv(skillsSourceEnv, sourceDir)

	cmd := Command()
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute list: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "team-custom") {
		t.Fatalf("expected override skill in output: %q", got)
	}
}

func TestInstallCommand_EmbeddedSkill(t *testing.T) {
	installDir := t.TempDir()

	cmd := Command()
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"install", "gcx", "--dir", installDir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute install: %v", err)
	}

	skillFile := filepath.Join(installDir, "gcx", "SKILL.md")
	data, err := os.ReadFile(skillFile)
	if err != nil {
		t.Fatalf("read installed SKILL.md: %v", err)
	}
	if !strings.Contains(string(data), "name: gcx") {
		t.Fatalf("installed file content mismatch: %q", string(data))
	}
}

func TestInstallCommandFailsIfAlreadyInstalledWithoutForce(t *testing.T) {
	sourceDir := t.TempDir()
	installDir := t.TempDir()
	writeSkill(t, sourceDir, "gcx", "gcx", "Main gcx skill")
	t.Setenv(skillsSourceEnv, sourceDir)

	cmd := Command()
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"install", "gcx", "--dir", installDir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("first install failed: %v", err)
	}

	cmd2 := Command()
	cmd2.SilenceErrors = true
	cmd2.SilenceUsage = true
	cmd2.SetOut(&bytes.Buffer{})
	cmd2.SetErr(&bytes.Buffer{})
	cmd2.SetArgs([]string{"install", "gcx", "--dir", installDir})
	err := cmd2.Execute()
	if err == nil {
		t.Fatalf("expected error on second install without --force")
	}
	if !strings.Contains(err.Error(), "already installed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReadSkillMetadata_FallbackParserHandlesColonInDescription(t *testing.T) {
	content := "---\n" +
		"name: debug-with-grafana\n" +
		"description: Triggers for: \"service is down\"\n" +
		"---\n\n" +
		"# Debug with Grafana\n"

	meta := readSkillMetadata([]byte(content))
	if meta.Name != "debug-with-grafana" {
		t.Fatalf("unexpected name: %q", meta.Name)
	}
	if !strings.Contains(meta.Description, "Triggers for:") {
		t.Fatalf("unexpected description: %q", meta.Description)
	}
}

func writeSkill(t *testing.T, root, dir, name, desc string) {
	t.Helper()
	skillDir := filepath.Join(root, dir)
	if err := os.MkdirAll(filepath.Join(skillDir, "references"), 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}

	content := "---\n" +
		"name: " + name + "\n" +
		"description: " + desc + "\n" +
		"---\n\n" +
		"# " + name + "\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "references", "notes.md"), []byte("reference"), 0o644); err != nil {
		t.Fatalf("write reference file: %v", err)
	}
}
