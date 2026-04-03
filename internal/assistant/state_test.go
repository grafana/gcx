package assistant_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/grafana/gcx/internal/assistant"
	"gopkg.in/yaml.v3"
)

func TestState_YAMLRoundTrip(t *testing.T) {
	original := assistant.State{LastContextID: "ctx-abc-123"}

	data, err := yaml.Marshal(&original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var loaded assistant.State
	if err := yaml.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if loaded.LastContextID != original.LastContextID {
		t.Errorf("LastContextID = %q, want %q", loaded.LastContextID, original.LastContextID)
	}
}

func TestState_SaveAndLoadFromDisk(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile := filepath.Join(tmpDir, "state.yaml")

	state := assistant.State{LastContextID: "ctx-test-456"}
	data, err := yaml.Marshal(&state)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	if err := os.WriteFile(stateFile, data, 0600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	readData, err := os.ReadFile(stateFile)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	var loaded assistant.State
	if err := yaml.Unmarshal(readData, &loaded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if loaded.LastContextID != "ctx-test-456" {
		t.Errorf("LastContextID = %q, want %q", loaded.LastContextID, "ctx-test-456")
	}
}

func TestState_EmptyLastContextID(t *testing.T) {
	state := assistant.State{}
	if state.LastContextID != "" {
		t.Errorf("empty State should have empty LastContextID, got %q", state.LastContextID)
	}
}
