package config

import (
	"errors"
	"fmt"
	"math"

	"github.com/goccy/go-yaml"
)

// declaredConfigVersion returns the explicitly declared version and whether a
// version key was present. Absence is distinct from an explicit zero: legacy
// files are unversioned, while every member of the v1 schema family must name
// a supported positive version.
func declaredConfigVersion(contents []byte) (int64, bool, error) {
	var raw map[string]any
	if err := yaml.Unmarshal(contents, &raw); err != nil {
		return 0, false, err
	}
	value, present := raw["version"]
	if !present {
		return 0, false, nil
	}

	switch version := value.(type) {
	case uint64:
		if version > math.MaxInt64 {
			return 0, true, errors.New("version is out of range")
		}
		return int64(version), true, nil
	case int64:
		return version, true, nil
	case int:
		return int64(version), true, nil
	default:
		return 0, true, errors.New("version must be an integer")
	}
}

func validateDeclaredConfigVersion(filename string, contents []byte) error {
	version, present, err := declaredConfigVersion(contents)
	if err != nil {
		// Let the strict schema decoder report general YAML syntax errors, but fail
		// a present malformed declaration here so a legacy-shaped document cannot
		// reach migration side effects with an ambiguous version.
		if present {
			return fmt.Errorf("invalid config version in %s: %w", filename, err)
		}
		return nil
	}
	if present && version != ConfigVersion {
		return UnsupportedVersionError{File: filename, Version: version}
	}
	return nil
}

func validateConfigForWrite(filename string, cfg *Config) error {
	if cfg.Version == 0 {
		cfg.Version = ConfigVersion
		return nil
	}
	if cfg.Version != ConfigVersion {
		return UnsupportedVersionError{File: filename, Version: cfg.Version}
	}
	return nil
}
