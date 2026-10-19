//go:build !wasip1

package ext_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/grafana/gcx/internal/ext"
)

func TestExtensionChild(t *testing.T) {
	i := slices.Index(os.Args, "--")
	if i < 0 {
		return
	}
	if err := json.NewEncoder(os.Stdout).Encode(map[string]any{"args": os.Args[i+1:], "context": os.Getenv("GCX_CONTEXT"), "binary": os.Getenv("GCX_EXT_GCX_BIN")}); err != nil {
		t.Fatal(err)
	}
	os.Exit(7)
}

func localSource(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		//nolint:gosec // Fixture arguments are controlled by the test.
		out, err := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git: %s %v", out, err)
		}
	}
	git("init", "--quiet")
	manifest := []byte("schemaVersion: 1\nname: sample\ndescription: Test\nversion: 0.1.0\nminGCXVersion: 0.0.0\nplatforms:\n  - selector: {os: '*', arch: '*'}\n    path: ./sample\n")
	if err := os.WriteFile(filepath.Join(dir, "gcx-extension.yaml"), manifest, 0600); err != nil {
		t.Fatal(err)
	}
	git("add", "gcx-extension.yaml")
	git("-c", "commit.gpgsign=false", "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--quiet", "-m", "add fixture")
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	//nolint:gosec // The fixture must execute as a child process.
	if err := os.WriteFile(filepath.Join(dir, "sample"), data, 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestInstallAndRun(t *testing.T) {
	source := localSource(t)
	if err := os.WriteFile(filepath.Join(source, "gcx-extension.yaml"), []byte("uncommitted: invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := ext.Prepare(t.Context(), source, "dev")
	if err != nil {
		t.Fatal(err)
	}
	store := &ext.Store{Dir: t.TempDir()}
	installed, err := store.Install(plan)
	if err != nil {
		t.Fatal(err)
	}
	if installed.Name != "sample" || installed.Commit == "" {
		t.Fatal(installed)
	}
	// Rebuilds must not change the already installed snapshot.
	if err := os.WriteFile(filepath.Join(source, "sample"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	output, err := os.CreateTemp(t.TempDir(), "result")
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	code, err := store.Run(t.Context(), "sample", ext.RunOptions{Args: []string{"-test.run=TestExtensionChild", "--", "--help", "a b"}, Binary: "/gcx", Context: "selected", Version: "dev", Stdout: output, Stderr: os.Stderr})
	if err != nil || code != 7 {
		t.Fatalf("code=%d err=%v", code, err)
	}
	data, err := os.ReadFile(output.Name())
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Args    []string `json:"args"`
		Context string   `json:"context"`
		Binary  string   `json:"binary"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Args, []string{"--help", "a b"}) || got.Context != "selected" || got.Binary != "/gcx" {
		t.Fatal(got)
	}
	lock, err := store.Lock(true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Install(plan); err == nil {
		t.Fatal("reinstalled while running")
	}
	_ = lock.Close()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if code, err := store.Run(ctx, "sample", ext.RunOptions{}); code != 5 || err == nil {
		t.Fatalf("code=%d err=%v", code, err)
	}
}

func TestNamesAndBounds(t *testing.T) {
	for _, name := range []string{"install", "help", "../sample", "search", "UPPER"} {
		if ext.ValidName(name) {
			t.Fatalf("accepted %q", name)
		}
	}
	for _, tc := range []struct {
		min, max, current string
		invalid           bool
	}{{"1.0.0", "2.0.0", "1.5.0", false}, {"1.0.0", "2.0.0", "2.1.0", true}, {"2.0.0", "1.0.0", "dev", true}, {"bad", "", "dev", true}} {
		if err := ext.CheckVersion(ext.Manifest{MinGCXVersion: tc.min, MaxGCXVersion: tc.max}, tc.current); (err != nil) != tc.invalid {
			t.Fatalf("case=%+v err=%v", tc, err)
		}
	}
}
