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

	"sigs.k8s.io/yaml"
)

// Top-level keys of a Mimir/Cortex Alertmanager user configuration. A plain
// Alertmanager configuration has neither.
const (
	userConfigAlertmanagerKey = "alertmanager_config"
	userConfigTemplatesKey    = "template_files"
)

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

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read input: %w", err)
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
	if err := yaml.UnmarshalStrict(data, &doc); err != nil {
		return AlertmanagerUserConfig{}, fmt.Errorf("failed to parse input: %w", err)
	}
	if doc == nil {
		return AlertmanagerUserConfig{}, errors.New("input is not an object")
	}

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
