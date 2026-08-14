package providers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"sigs.k8s.io/yaml"
)

// ReadFileOrStdin decodes a JSON/YAML object from a file path or stdin (when
// file == "-") into the provided target. Used by provider commands that post
// bare objects to a product REST API.
//
// The target is the bare object, but the manifests that gcx itself writes —
// `resources list-examples`, `pull`, and the K8s envelope that provider list
// commands emit — wrap that object in an apiVersion/kind/metadata/spec
// envelope. Decoding such an envelope into the bare target matches no field
// and silently produces an empty object, so unwrap the envelope first. See
// issue #1185.
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

	if spec, ok := envelopeSpec(data); ok {
		data = spec
	}

	if err := yaml.Unmarshal(data, out); err != nil {
		return fmt.Errorf("failed to parse input: %w", err)
	}
	return nil
}

// envelopeSpec reports whether data is a K8s-style resource envelope and
// returns its spec as JSON. A document counts as an envelope only when it
// carries a non-null object-valued "spec" together with "apiVersion" or
// "kind", so a bare object with its own "spec" field passes through
// unchanged.
func envelopeSpec(data []byte) ([]byte, bool) {
	var doc map[string]json.RawMessage
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, false
	}

	_, hasAPIVersion := doc["apiVersion"]
	_, hasKind := doc["kind"]
	if !hasAPIVersion && !hasKind {
		return nil, false
	}

	spec, ok := doc["spec"]
	if !ok {
		return nil, false
	}

	var specObject map[string]json.RawMessage
	if err := json.Unmarshal(spec, &specObject); err != nil || specObject == nil {
		return nil, false
	}
	return spec, true
}
