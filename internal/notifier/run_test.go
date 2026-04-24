package notifier

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func testRunSkillsFS() fstest.MapFS {
	return fstest.MapFS{
		"alpha/SKILL.md":            {Data: []byte("alpha-skill")},
		"alpha/references/guide.md": {Data: []byte("alpha-guide")},
	}
}

func TestMaybeNotifySkillsAt_WritesMessageAndStateWhenDue(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), ".agents")
	statePath := filepath.Join(t.TempDir(), "notifier.yml")
	now := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)

	if err := os.MkdirAll(filepath.Join(root, "skills", "alpha"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "skills", "alpha", "SKILL.md"), []byte("local-change"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	var out bytes.Buffer
	if err := maybeNotifySkillsAt(testRunSkillsFS(), &out, statePath, root, now, 24*time.Hour); err != nil {
		t.Fatalf("maybeNotifySkillsAt() error = %v", err)
	}
	if !strings.Contains(out.String(), "Run: gcx skills update") {
		t.Fatalf("output = %q, want skills update hint", out.String())
	}

	state, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState() error = %v", err)
	}
	if got := state.Checks[SkillsCheckKey].LastCheckedAt; !got.Equal(now) {
		t.Fatalf("last checked = %v, want %v", got, now)
	}
}

func TestMaybeNotifySkillsAt_SkipsWhenNotDue(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), ".agents")
	statePath := filepath.Join(t.TempDir(), "notifier.yml")
	now := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)

	state := State{Checks: map[string]CheckState{
		SkillsCheckKey: {LastCheckedAt: now.Add(-time.Hour)},
	}}
	if err := SaveState(statePath, state); err != nil {
		t.Fatalf("SaveState() error = %v", err)
	}

	var out bytes.Buffer
	if err := maybeNotifySkillsAt(testRunSkillsFS(), &out, statePath, root, now, 24*time.Hour); err != nil {
		t.Fatalf("maybeNotifySkillsAt() error = %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("output = %q, want empty", out.String())
	}

	loaded, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState() error = %v", err)
	}
	if got := loaded.Checks[SkillsCheckKey].LastCheckedAt; !got.Equal(now.Add(-time.Hour)) {
		t.Fatalf("last checked = %v, want unchanged %v", got, now.Add(-time.Hour))
	}
}

func TestMaybeNotifySkillsAt_NoUpdateNeededMarksStateWithoutOutput(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), ".agents")
	statePath := filepath.Join(t.TempDir(), "notifier.yml")
	now := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)

	if err := os.MkdirAll(filepath.Join(root, "skills", "alpha", "references"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "skills", "alpha", "SKILL.md"), []byte("alpha-skill"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "skills", "alpha", "references", "guide.md"), []byte("alpha-guide"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	var out bytes.Buffer
	if err := maybeNotifySkillsAt(testRunSkillsFS(), &out, statePath, root, now, 24*time.Hour); err != nil {
		t.Fatalf("maybeNotifySkillsAt() error = %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("output = %q, want empty", out.String())
	}

	state, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState() error = %v", err)
	}
	if got := state.Checks[SkillsCheckKey].LastCheckedAt; !got.Equal(now) {
		t.Fatalf("last checked = %v, want %v", got, now)
	}
}
