package native

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/grafana/gcx/internal/format"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// maxStdinManifestBytes caps manifests read from standard input.
const maxStdinManifestBytes = 32 << 20

// ReadManifest reads a JSON or YAML manifest into an unstructured object from
// filename, or from stdin when filename is "-".
func ReadManifest(filename string, stdin io.Reader) (*unstructured.Unstructured, error) {
	if filename == "" {
		return nil, errors.New("--filename / -f is required")
	}

	if filename == "-" {
		if stdin == nil {
			return nil, errors.New("no standard input available")
		}
		return decodeManifest(io.LimitReader(stdin, maxStdinManifestBytes))
	}

	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open %q: %w", filename, err)
	}
	defer f.Close()

	return decodeManifest(f)
}

// decodeManifest decodes JSON when the input starts with '{' or '[', and YAML
// otherwise.
func decodeManifest(r io.Reader) (*unstructured.Unstructured, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest: %w", err)
	}

	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, errors.New("manifest is empty")
	}

	if trimmed[0] == '{' || trimmed[0] == '[' {
		obj := &unstructured.Unstructured{}
		if err := obj.UnmarshalJSON(trimmed); err != nil {
			return nil, fmt.Errorf("failed to parse JSON manifest: %w", err)
		}
		return obj, nil
	}

	var raw map[string]any
	if err := format.NewYAMLCodec().Decode(bytes.NewReader(trimmed), &raw); err != nil {
		return nil, fmt.Errorf("manifest is neither valid JSON nor YAML: %w", err)
	}
	if len(raw) == 0 {
		return nil, errors.New("manifest is empty")
	}

	return &unstructured.Unstructured{Object: raw}, nil
}
