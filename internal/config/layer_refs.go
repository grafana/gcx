package config

import (
	"fmt"
	"os"

	"github.com/goccy/go-yaml"
)

// ContextDefinedInOtherLayer reports whether any discovered config layer
// other than the one at targetPath defines a context called name. Layers are
// read without resolving credentials. Layering bypassed by an explicit file or
// $GCX_CONFIG has no other layer.
func ContextDefinedInOtherLayer(explicitFile, targetPath, name string) (bool, error) {
	if explicitFile != "" || os.Getenv(ConfigFileEnvVar) != "" {
		return false, nil
	}
	sources, err := DiscoverSources()
	if err != nil {
		return false, err
	}
	for _, source := range sources {
		if source.Path == targetPath {
			continue
		}
		contents, err := readConfigSource(source)
		if err != nil {
			return false, err
		}
		var layer struct {
			Contexts map[string]any `yaml:"contexts"`
		}
		if err := yaml.Unmarshal(contents, &layer); err != nil {
			return false, fmt.Errorf("read %s: %w", source.Path, err)
		}
		if _, ok := layer.Contexts[name]; ok {
			return true, nil
		}
	}
	return false, nil
}
