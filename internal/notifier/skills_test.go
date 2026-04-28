package notifier //nolint:testpackage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func testSkillsFS() fstest.MapFS {
	return fstest.MapFS{
		"alpha/SKILL.md":            {Data: []byte("alpha-skill")},
		"alpha/references/guide.md": {Data: []byte("alpha-guide")},
		"beta/SKILL.md":             {Data: []byte("beta-skill")},
	}
}

func TestSkillsUpdateMessage_NoInstalledSkillsReturnsEmpty(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), ".agents")
	msg, err := SkillsUpdateMessage(testSkillsFS(), root)
	if err != nil {
		t.Fatalf("SkillsUpdateMessage() error = %v", err)
	}
	if msg != "" {
		t.Fatalf("SkillsUpdateMessage() = %q, want empty", msg)
	}
}

func TestSkillsUpdateMessage_InstalledSkillMatchesBundleReturnsEmpty(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), ".agents")
	if err := os.MkdirAll(filepath.Join(root, "skills", "alpha", "references"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "skills", "alpha", "SKILL.md"), []byte("alpha-skill"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "skills", "alpha", "references", "guide.md"), []byte("alpha-guide"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	msg, err := SkillsUpdateMessage(testSkillsFS(), root)
	if err != nil {
		t.Fatalf("SkillsUpdateMessage() error = %v", err)
	}
	if msg != "" {
		t.Fatalf("SkillsUpdateMessage() = %q, want empty", msg)
	}
}

func TestSkillsUpdateMessage_InstalledSkillDiffersFromBundle(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), ".agents")
	if err := os.MkdirAll(filepath.Join(root, "skills", "alpha"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "skills", "alpha", "SKILL.md"), []byte("local-change"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	msg, err := SkillsUpdateMessage(testSkillsFS(), root)
	if err != nil {
		t.Fatalf("SkillsUpdateMessage() error = %v", err)
	}
	if msg == "" {
		t.Fatal("SkillsUpdateMessage() = empty, want message")
	}
	if want := "Run: gcx skills update"; !strings.HasSuffix(msg, want) {
		t.Fatalf("SkillsUpdateMessage() = %q, want suffix %q", msg, want)
	}
}
