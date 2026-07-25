package providers

import (
	"strings"

	"github.com/grafana/gcx/internal/config"
)

const providerEnvironmentPrefix = "GRAFANA_PROVIDER_"

// IsBlankProviderCredentialEnvironmentOverride reports whether a provider
// environment variable is blank and names a credential field. In addition to
// the trust-bound Synthetic Monitoring token, it honors Secret metadata from
// registered providers so future credential fields inherit the same behavior.
// Unknown and explicitly non-secret provider fields retain their existing
// blank-value semantics.
func IsBlankProviderCredentialEnvironmentOverride(envKey, value string) bool {
	return isBlankProviderCredentialEnvironmentOverride(envKey, value, All())
}

func isBlankProviderCredentialEnvironmentOverride(envKey, value string, registered []Provider) bool {
	if strings.TrimSpace(value) != "" {
		return false
	}
	if config.IsBlankCredentialEnvironmentOverride(envKey, value) {
		return true
	}

	providerName, configKey, ok := parseProviderEnvironmentKey(envKey)
	if !ok {
		return false
	}
	for _, provider := range registered {
		if provider.Name() != providerName {
			continue
		}
		for _, key := range provider.ConfigKeys() {
			if key.Name == configKey {
				return key.Secret
			}
		}
		return false
	}
	return false
}

func parseProviderEnvironmentKey(envKey string) (string, string, bool) {
	if !strings.HasPrefix(envKey, providerEnvironmentPrefix) {
		return "", "", false
	}
	suffix := strings.TrimPrefix(envKey, providerEnvironmentPrefix)
	nameParts := strings.SplitN(suffix, "_", 2)
	if len(nameParts) != 2 || nameParts[0] == "" || nameParts[1] == "" {
		return "", "", false
	}
	providerName := strings.ToLower(nameParts[0])
	configKey := strings.ReplaceAll(strings.ToLower(nameParts[1]), "_", "-")
	return providerName, configKey, true
}
