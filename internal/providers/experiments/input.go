package experiments

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	yamlv3 "gopkg.in/yaml.v3"
	"sigs.k8s.io/yaml"
)

const maxManifestBytes = 4 << 20

// readManifest reads one complete Experiment resource from YAML or JSON.
// Odin performs the authoritative structural and semantic validation.
func readManifest(path string, stdin io.Reader) ([]byte, error) {
	encoded, err := readExperimentDocument(path, stdin)
	if err != nil {
		return nil, err
	}
	var manifest struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
		Metadata   struct {
			Name            string `json:"name"`
			UID             string `json:"uid"`
			ResourceVersion string `json:"resourceVersion"`
		} `json:"metadata"`
		Spec   json.RawMessage `json:"spec"`
		Status json.RawMessage `json:"status"`
	}
	if err := json.Unmarshal(encoded, &manifest); err != nil {
		return nil, fmt.Errorf("decode experiment manifest: %w", err)
	}
	if manifest.APIVersion != "odin.ext.grafana.com/v1alpha1" || manifest.Kind != "Experiment" {
		return nil, errors.New("experiment manifest requires apiVersion odin.ext.grafana.com/v1alpha1 and kind Experiment")
	}
	if strings.TrimSpace(manifest.Metadata.Name) == "" {
		return nil, errors.New("experiment manifest requires metadata.name")
	}
	if manifest.Metadata.UID != "" || manifest.Metadata.ResourceVersion != "" || len(manifest.Status) > 0 {
		return nil, errors.New("new experiment must omit metadata.uid, metadata.resourceVersion, and top-level status")
	}
	if len(manifest.Spec) == 0 || bytes.Equal(bytes.TrimSpace(manifest.Spec), []byte("null")) {
		return nil, errors.New("experiment manifest requires spec")
	}
	return encoded, nil
}

func readExperimentDocument(path string, stdin io.Reader) ([]byte, error) {
	var reader io.Reader
	if path == "-" {
		reader = stdin
		if reader == nil {
			return nil, errors.New("stdin is unavailable")
		}
	} else {
		file, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open experiment manifest %q: %w", path, err)
		}
		defer file.Close()
		reader = file
	}

	data, err := io.ReadAll(io.LimitReader(reader, maxManifestBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read experiment manifest: %w", err)
	}
	if len(data) > maxManifestBytes {
		return nil, fmt.Errorf("experiment manifest exceeds %d bytes", maxManifestBytes)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errors.New("experiment manifest is empty")
	}
	decoder := yamlv3.NewDecoder(bytes.NewReader(data))
	var first, second yamlv3.Node
	if err := decoder.Decode(&first); err != nil {
		return nil, fmt.Errorf("parse experiment manifest: %w", err)
	}
	if err := decoder.Decode(&second); !errors.Is(err, io.EOF) {
		if err != nil {
			return nil, fmt.Errorf("parse experiment manifest: %w", err)
		}
		return nil, errors.New("experiment manifest contains multiple YAML documents; provide one Experiment")
	}

	encoded, err := yaml.YAMLToJSONStrict(data)
	if err != nil {
		return nil, fmt.Errorf("parse experiment manifest: %w", err)
	}
	return encoded, nil
}

func readUpdateManifest(path string, stdin io.Reader, name string) ([]byte, error) {
	encoded, err := readExperimentDocument(path, stdin)
	if err != nil {
		return nil, err
	}
	var manifest struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
		Metadata   struct {
			Name            string `json:"name"`
			Namespace       string `json:"namespace"`
			ResourceVersion string `json:"resourceVersion"`
		} `json:"metadata"`
		Spec json.RawMessage `json:"spec"`
	}
	if err := json.Unmarshal(encoded, &manifest); err != nil {
		return nil, fmt.Errorf("decode experiment manifest: %w", err)
	}
	if manifest.APIVersion != "odin.ext.grafana.com/v1alpha1" || manifest.Kind != "Experiment" {
		return nil, errors.New("experiment manifest requires apiVersion odin.ext.grafana.com/v1alpha1 and kind Experiment")
	}
	if manifest.Metadata.Name != name {
		return nil, fmt.Errorf("manifest metadata.name %q does not match experiment %q", manifest.Metadata.Name, name)
	}
	if manifest.Metadata.Namespace == "" || manifest.Metadata.ResourceVersion == "" {
		return nil, errors.New("update requires metadata.namespace and metadata.resourceVersion from gcx experiments get")
	}
	if len(manifest.Spec) == 0 || bytes.Equal(bytes.TrimSpace(manifest.Spec), []byte("null")) {
		return nil, errors.New("experiment manifest requires spec")
	}
	return encoded, nil
}
