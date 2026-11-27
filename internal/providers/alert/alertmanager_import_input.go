package alert

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	yamlv3 "gopkg.in/yaml.v3"
	"sigs.k8s.io/yaml"
)

// Top-level keys of a Mimir/Cortex Alertmanager user configuration. A plain
// Alertmanager configuration has neither.
const (
	userConfigAlertmanagerKey = "alertmanager_config"
	userConfigTemplatesKey    = "template_files"
)

// alertmanagerImportFileLimit caps input at 10 MiB before parsing, as rule imports do.
const alertmanagerImportFileLimit = 10 << 20

// readAlertmanagerUserConfig reads the import input from file, or from stdin
// when file is "-", and adds the template files at templatePaths.
//
// The input is one of two formats:
//   - a Mimir/Cortex user configuration, which has alertmanager_config (the
//     Alertmanager configuration as a string) and optional template_files.
//     This is what Mimir's GET /api/v1/alerts returns. It is used as it is.
//   - a plain Alertmanager configuration (route, receivers, ...). Its bytes
//     are sent unchanged as alertmanager_config.
//
// A template file name that occurs two times is an error.
func readAlertmanagerUserConfig(file string, stdin io.Reader, templatePaths []string) (AlertmanagerUserConfig, error) {
	data, err := readImportInput(file, stdin)
	if err != nil {
		return AlertmanagerUserConfig{}, err
	}

	cfg, err := parseAlertmanagerUserConfig(data)
	if err != nil {
		return AlertmanagerUserConfig{}, fmt.Errorf("%s: %w", importInputName(file), err)
	}

	if err := addTemplateFiles(&cfg, importInputName(file), templatePaths); err != nil {
		return AlertmanagerUserConfig{}, err
	}
	return cfg, nil
}

// readImportInput returns the raw bytes of file, or of stdin when file is "-".
// It does not use providers.ReadFileOrStdin because that function parses YAML,
// but a plain Alertmanager configuration must remain byte-for-byte unchanged.
func readImportInput(file string, stdin io.Reader) ([]byte, error) {
	if file == "" {
		return nil, errors.New("--filename is required (use - to read from stdin)")
	}

	reader := stdin
	if file != "-" {
		f, err := os.Open(file)
		if err != nil {
			return nil, fmt.Errorf("failed to open file %s: %w", file, err)
		}
		defer f.Close()
		reader = f
	}

	data, err := io.ReadAll(io.LimitReader(reader, alertmanagerImportFileLimit+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read input: %w", err)
	}
	if len(data) > alertmanagerImportFileLimit {
		return nil, fmt.Errorf("%s: input exceeds the %d MiB limit", importInputName(file), alertmanagerImportFileLimit>>20)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errors.New("input is empty")
	}
	return data, nil
}

// importInputName names the source of the input for an error message.
func importInputName(file string) string {
	if file == "-" {
		return "stdin"
	}
	return file
}

// parseAlertmanagerUserConfig detects the input format and returns the user
// configuration to send.
func parseAlertmanagerUserConfig(data []byte) (AlertmanagerUserConfig, error) {
	var doc map[string]json.RawMessage
	strictErr := yaml.UnmarshalStrict(data, &doc)
	if normalized := normalizeAlertmanagerTemplateNames(data); normalized != nil {
		var normalizedDoc map[string]json.RawMessage
		normalizedErr := yaml.UnmarshalStrict(normalized, &normalizedDoc)
		// Validate template names as text even if the original decode cannot
		// represent a name as a JSON key. Keep the original error if both fail.
		if strictErr == nil || normalizedErr == nil {
			doc, strictErr = normalizedDoc, normalizedErr
		}
	}
	if strictErr != nil {
		return AlertmanagerUserConfig{}, fmt.Errorf("failed to parse input: %w", strictErr)
	}
	if doc == nil {
		return AlertmanagerUserConfig{}, errors.New("input is not an object")
	}

	// Ignore extra wrapper keys to match Mimir's user-config decoder.
	rawConfig, isUserConfig := doc[userConfigAlertmanagerKey]
	rawTemplates, hasTemplates := doc[userConfigTemplatesKey]
	if !isUserConfig && !hasTemplates {
		// A plain Alertmanager configuration: keep the bytes the user wrote.
		return AlertmanagerUserConfig{AlertmanagerConfig: string(data)}, nil
	}

	var cfg AlertmanagerUserConfig
	if err := json.Unmarshal(rawConfig, &cfg.AlertmanagerConfig); err != nil || cfg.AlertmanagerConfig == "" {
		return AlertmanagerUserConfig{}, fmt.Errorf(
			"%s must be a non-empty string that holds the Alertmanager configuration", userConfigAlertmanagerKey)
	}
	if hasTemplates {
		if err := json.Unmarshal(rawTemplates, &cfg.TemplateFiles); err != nil {
			return AlertmanagerUserConfig{}, fmt.Errorf(
				"%s must map template file names to their contents: %w", userConfigTemplatesKey, err)
		}
	}
	return cfg, nil
}

// normalizeAlertmanagerTemplateNames makes the strict decoder read template names as
// strings, like Mimir. It does not change the interpretation of template values.
func normalizeAlertmanagerTemplateNames(data []byte) []byte {
	doc := new(yamlv3.Node)
	if err := yamlv3.Unmarshal(data, doc); err != nil || len(doc.Content) == 0 {
		return nil
	}

	type role uint8
	const (
		other role = 1 << iota
		wrapper
		templates
		name
	)
	seen := make(map[*yamlv3.Node]role)
	var visit func(*yamlv3.Node, role)
	visit = func(node *yamlv3.Node, at role) {
		if seen[node]&at != 0 {
			return
		}
		seen[node] |= at
		switch {
		case node.Kind == yamlv3.AliasNode:
			visit(node.Alias, at)
		case node.Kind == yamlv3.MappingNode && (at == wrapper || at == templates):
			for i := 0; i < len(node.Content); i += 2 {
				key, value := node.Content[i], node.Content[i+1]
				switch {
				case key.ShortTag() == "!!merge" && key.Kind == yamlv3.ScalarNode:
					visit(value, at)
				case at == templates:
					visit(key, name)
					visit(value, other)
				default:
					visit(key, other)
					if key.Kind == yamlv3.AliasNode {
						key = key.Alias
					}
					if key.Kind == yamlv3.ScalarNode && key.ShortTag() == "!!str" && key.Value == userConfigTemplatesKey {
						visit(value, templates)
					} else {
						visit(value, other)
					}
				}
			}
		default:
			if node.Kind != yamlv3.SequenceNode || at == name {
				at = other
			}
			for _, child := range node.Content {
				visit(child, at)
			}
		}
	}
	visit(doc.Content[0], wrapper)

	changed := false
	for node, at := range seen {
		if at&name == 0 || node.Kind == yamlv3.AliasNode {
			continue
		}
		if node.Kind != yamlv3.ScalarNode {
			return nil // Keep legacy handling of complex names.
		}
		if !alertmanagerTemplateNameNeedsQuoting(node) {
			continue
		}
		// Do not change an anchor that is also used outside template names.
		if at != name {
			return nil
		}
		node.Tag, node.Style = "!!str", yamlv3.DoubleQuotedStyle
		changed = true
	}
	if !changed {
		return nil
	}
	normalized, err := yamlv3.Marshal(doc)
	if err != nil {
		return nil
	}
	return normalized
}

func alertmanagerTemplateNameNeedsQuoting(node *yamlv3.Node) bool {
	switch node.ShortTag() {
	case "!!str":
		if node.Style == 0 {
			switch strings.ToLower(node.Value) {
			case "y", "yes", "n", "no", "on", "off":
				return true
			}
		}
	case "!!bool", "!!int", "!!float", "!!timestamp":
		if node.Style&yamlv3.TaggedStyle == 0 {
			return true
		}
		// Check explicit tags before replacing them with a string tag.
		scalar, err := yamlv3.Marshal(node)
		return err == nil && yaml.UnmarshalStrict(scalar, new(string)) == nil
	}
	return false // Leave unhandled tags to the strict decoder.
}

// addTemplateFiles adds the template files at paths to cfg. Each path must be
// a file, not a directory. The base name of each file is its template name.
func addTemplateFiles(cfg *AlertmanagerUserConfig, inputName string, paths []string) error {
	sources := make(map[string]string, len(cfg.TemplateFiles))
	for name := range cfg.TemplateFiles {
		sources[name] = userConfigTemplatesKey + " in " + inputName
	}

	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			if pathErr, ok := errors.AsType[*fs.PathError](err); ok {
				err = pathErr.Err
			}
			return fmt.Errorf("--template %s: %w", path, err)
		}
		if info.IsDir() {
			return fmt.Errorf("--template %s: is a directory; pass template files (for example %s/*.tmpl)", path, path)
		}

		name := filepath.Base(path)
		if source, ok := sources[name]; ok {
			return fmt.Errorf("template %q from %s is already defined by %s", name, path, source)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read template file %s: %w", path, err)
		}
		if cfg.TemplateFiles == nil {
			cfg.TemplateFiles = make(map[string]string)
		}
		cfg.TemplateFiles[name] = string(content)
		sources[name] = path
	}
	return nil
}
