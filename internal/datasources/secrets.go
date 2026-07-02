package datasources

import (
	"fmt"
	"log/slog"
	"maps"
	"os"
	"strings"

	"github.com/goccy/go-yaml"
)

// ResolveSecrets resolves every entry in the manifest's secure block to an
// inline value, ready to be sent to the server. When secretsFile is non-empty,
// its entries are merged into the secure block first (the file wins on
// conflict). Each secure key may set exactly one source — create / fromEnv /
// fromFile — or remove. A referenced env var or file that is missing/empty is a
// hard error so an empty secret is never written silently.
func (m *DataSourceManifest) ResolveSecrets(secretsFile string) error {
	if secretsFile != "" {
		if err := m.mergeSecretsFile(secretsFile); err != nil {
			return err
		}
	}

	for key, sv := range m.Secure {
		keep, err := resolveSecureValue(key, &sv)
		if err != nil {
			return err
		}
		if !keep {
			// Read-back placeholder (only name set): leave the stored secret
			// unchanged by dropping it from the write payload.
			delete(m.Secure, key)
			continue
		}
		m.Secure[key] = sv
	}
	return nil
}

// resolveSecureValue resolves a secure entry to an inline value. It returns
// keep=false when the entry carries no write source (a read-back placeholder
// with only name set), meaning the stored secret should be left unchanged.
func resolveSecureValue(key string, sv *SecureValue) (bool, error) {
	sources := 0
	if sv.Create != "" {
		sources++
	}
	if sv.FromEnv != "" {
		sources++
	}
	if sv.FromFile != "" {
		sources++
	}

	if sv.Remove {
		if sources > 0 {
			return false, fmt.Errorf("secure.%s: remove cannot be combined with a value source", key)
		}
		return true, nil
	}
	if sources == 0 {
		if sv.Name != "" {
			return false, nil // round-trip placeholder: keep the existing secret
		}
		return false, fmt.Errorf("secure.%s: one of create, fromEnv, or fromFile is required", key)
	}
	if sources > 1 {
		return false, fmt.Errorf("secure.%s: only one of create, fromEnv, or fromFile may be set", key)
	}

	switch {
	case sv.FromEnv != "":
		val, ok := os.LookupEnv(sv.FromEnv)
		if !ok || val == "" {
			return false, fmt.Errorf("secure.%s: environment variable %q is not set or empty", key, sv.FromEnv)
		}
		sv.Create = val
	case sv.FromFile != "":
		data, err := os.ReadFile(sv.FromFile)
		if err != nil {
			return false, fmt.Errorf("secure.%s: reading %q: %w", key, sv.FromFile, err)
		}
		val := strings.TrimRight(string(data), "\r\n")
		if val == "" {
			return false, fmt.Errorf("secure.%s: file %q is empty", key, sv.FromFile)
		}
		sv.Create = val
	}

	// Clear the indirection markers now that the value is resolved.
	sv.FromEnv = ""
	sv.FromFile = ""
	return true, nil
}

func (m *DataSourceManifest) mergeSecretsFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading secrets file %q: %w", path, err)
	}
	var fileSecrets map[string]SecureValue
	if err := yaml.Unmarshal(data, &fileSecrets); err != nil {
		return fmt.Errorf("parsing secrets file %q: %w", path, err)
	}
	if m.Secure == nil {
		m.Secure = make(map[string]SecureValue, len(fileSecrets))
	}
	maps.Copy(m.Secure, fileSecrets)
	return nil
}

// secretLikelyRequired reports whether ds is being written without any secret
// even though its configuration clearly needs one — basic auth is enabled but
// no secure value is supplied (the password would silently be dropped). It is a
// deliberately narrow, type-agnostic signal used only to warn, never to block.
func secretLikelyRequired(ds *Datasource) bool {
	return ds.BasicAuth && len(ds.SecureJSONData) == 0
}

// WarnIfSecretMissing emits a warning when a write is unlikely to carry a
// credential the datasource needs. It does not prevent the write.
func WarnIfSecretMissing(ds *Datasource) {
	if !secretLikelyRequired(ds) {
		return
	}
	slog.Warn(
		"basic auth is enabled but no secret was supplied; the password is write-only and will not be set — add a secure block (e.g. basicAuthPassword) to configure it",
		"name", ds.Name,
		"type", ds.Type,
		"uid", ds.UID,
	)
}
