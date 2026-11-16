package alert

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

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
	if err := yaml.Unmarshal(data, &doc); err != nil {
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

// addTemplateFiles adds the template files at paths to cfg. A path is a file,
// or a directory whose regular files (not recursive) are added, except the
// names that start with ".". The base name of each file is its template name.
func addTemplateFiles(cfg *AlertmanagerUserConfig, inputName string, paths []string) error {
	sources := make(map[string]string, len(cfg.TemplateFiles))
	for name := range cfg.TemplateFiles {
		sources[name] = userConfigTemplatesKey + " in " + inputName
	}

	for _, path := range paths {
		files, err := templateFilePaths(path)
		if err != nil {
			return err
		}
		for _, file := range files {
			name := filepath.Base(file)
			if source, ok := sources[name]; ok {
				return fmt.Errorf("template %q from %s is already defined by %s", name, file, source)
			}
			content, err := os.ReadFile(file)
			if err != nil {
				return fmt.Errorf("failed to read template file %s: %w", file, err)
			}
			if cfg.TemplateFiles == nil {
				cfg.TemplateFiles = make(map[string]string)
			}
			cfg.TemplateFiles[name] = string(content)
			sources[name] = file
		}
	}
	return nil
}

// templateFilePaths returns path when it is a regular file, or the regular
// files directly in path when it is a directory. In a directory, it skips the
// names that start with ".", such as editor swap files and .DS_Store.
func templateFilePaths(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read template path %s: %w", path, err)
	}
	if !info.IsDir() {
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("template path %s is not a regular file or a directory", path)
		}
		return []string{path}, nil
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read template directory %s: %w", path, err)
	}
	var files []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		file := filepath.Join(path, entry.Name())
		// Stat follows symbolic links, so a link to a regular file counts.
		entryInfo, err := os.Stat(file)
		if err != nil {
			return nil, fmt.Errorf("failed to read template file %s: %w", file, err)
		}
		if entryInfo.Mode().IsRegular() {
			files = append(files, file)
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("template directory %s contains no regular files (names that start with \".\" are skipped)", path)
	}
	return files, nil
}
