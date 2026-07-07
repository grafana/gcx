package crudcmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/goccy/go-yaml"
	sigsyaml "sigs.k8s.io/yaml"
)

// ReadFile reads raw bytes from path, or from stdin when path == "-". This is
// the file-or-stdin convention shared by every create/update command's
// -f/--filename flag.
func ReadFile(path string, stdin io.Reader) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(stdin)
	}
	return os.ReadFile(path)
}

// ReadJSONOrYAMLFile reads path (or stdin, for "-") and decodes it into a T,
// trying JSON first and falling back to YAML (via goccy/go-yaml) on failure.
// This matches the ReadXFile helpers duplicated across aio11y/eval packages
// (guards, rules, evaluators, ...): bare object, no Kubernetes envelope.
func ReadJSONOrYAMLFile[T any](path string, stdin io.Reader) (*T, error) {
	data, err := ReadFile(path, stdin)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var v T
	if jsonErr := json.Unmarshal(data, &v); jsonErr == nil {
		return &v, nil
	}

	var yv T
	if err := yaml.Unmarshal(data, &yv); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return &yv, nil
}

// ReadYAMLOrJSONFile reads path (or stdin, for "-") and decodes it into a T
// via sigs.k8s.io/yaml, which transparently handles both YAML and JSON
// input. It rejects an empty filename or empty input, matching the
// provisioning-style readers (bare object, no envelope) used by the alert
// provider's contact-points/mute-timings/templates commands.
func ReadYAMLOrJSONFile[T any](path string, stdin io.Reader) (*T, error) {
	if path == "" {
		return nil, errors.New("--filename is required (use - to read from stdin)")
	}

	data, err := ReadFile(path, stdin)
	if err != nil {
		return nil, fmt.Errorf("failed to read input: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, errors.New("input is empty")
	}

	var v T
	if err := sigsyaml.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("failed to parse input: %w", err)
	}
	return &v, nil
}
