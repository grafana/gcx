package providers

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"sigs.k8s.io/yaml"
)

// ReadFileOrStdin decodes a plain JSON/YAML object from a file path or stdin
// (when file == "-") into the provided target. Used by provider commands that
// post bare objects (no K8s apiVersion/kind/spec wrapper).
func ReadFileOrStdin(file string, stdin io.Reader, out any) error {
	if file == "" {
		return errors.New("--filename is required (use - to read from stdin)")
	}

	var reader io.Reader
	if file == "-" {
		reader = stdin
	} else {
		f, err := os.Open(file)
		if err != nil {
			return fmt.Errorf("failed to open file %s: %w", file, err)
		}
		defer f.Close()
		reader = f
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("failed to read input: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return errors.New("input is empty")
	}

	if err := yaml.Unmarshal(data, out); err != nil {
		return fmt.Errorf("failed to parse input: %w", err)
	}
	return nil
}
