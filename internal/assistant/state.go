package assistant

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// State represents runtime state that persists across sessions.
type State struct {
	// LastContextID is the context ID of the last chat session.
	LastContextID string `yaml:"last-context-id,omitempty"`
}

// statePath returns the path to the assistant state file.
func statePath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("failed to get config directory: %w", err)
	}
	return filepath.Join(configDir, "gcx", "assistant-state.yaml"), nil
}

// LoadState loads the state from the state file.
func LoadState() (*State, error) {
	path, err := statePath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &State{}, nil
		}
		return nil, fmt.Errorf("failed to read state file: %w", err)
	}

	var state State
	if err := yaml.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to parse state file: %w", err)
	}

	return &state, nil
}

// Save saves the state to the state file.
func (s *State) Save() error {
	path, err := statePath()
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}

	data, err := yaml.Marshal(s)
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write state file: %w", err)
	}

	return nil
}

// SaveLastContextID saves the last context ID to state.
func SaveLastContextID(contextID string) error {
	state, err := LoadState()
	if err != nil {
		state = &State{}
	}
	state.LastContextID = contextID
	return state.Save()
}

// GetLastContextID returns the last context ID from state.
func GetLastContextID() (string, error) {
	state, err := LoadState()
	if err != nil {
		return "", err
	}
	if state.LastContextID == "" {
		return "", errors.New("no previous chat session found")
	}
	return state.LastContextID, nil
}
