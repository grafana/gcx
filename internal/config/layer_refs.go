package config

import (
	"fmt"
	"os"

	"github.com/goccy/go-yaml"
)

// OtherLayerRefs describes what config layers other than a write target still
// define: the context names and the stack and cloud entries those contexts
// reference.
type OtherLayerRefs struct {
	Contexts map[string]bool
	Stacks   map[string]bool
	Clouds   map[string]bool
}

// OtherLayerReferences reads, without resolving credentials, every discovered
// config layer except the one at targetPath. It returns an empty result when
// layering is bypassed by an explicit file or $GCX_CONFIG.
func OtherLayerReferences(explicitFile, targetPath string) (OtherLayerRefs, error) {
	refs := OtherLayerRefs{
		Contexts: map[string]bool{},
		Stacks:   map[string]bool{},
		Clouds:   map[string]bool{},
	}
	if explicitFile != "" || os.Getenv(ConfigFileEnvVar) != "" {
		return refs, nil
	}

	sources, err := DiscoverSources()
	if err != nil {
		return refs, err
	}
	for _, source := range sources {
		if source.Path == targetPath {
			continue
		}
		contents, err := readConfigSource(source)
		if err != nil {
			return refs, err
		}
		var layer struct {
			Contexts map[string]*struct {
				Stack string `yaml:"stack"`
				Cloud string `yaml:"cloud"`
			} `yaml:"contexts"`
		}
		if err := yaml.Unmarshal(contents, &layer); err != nil {
			return refs, fmt.Errorf("read %s: %w", source.Path, err)
		}
		for name, c := range layer.Contexts {
			if c == nil {
				continue
			}
			refs.Contexts[name] = true
			if c.Stack != "" {
				refs.Stacks[c.Stack] = true
			}
			if c.Cloud != "" {
				refs.Clouds[c.Cloud] = true
			}
		}
	}
	return refs, nil
}
