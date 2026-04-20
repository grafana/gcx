package framework

import (
	"strings"

	"github.com/grafana/gcx/internal/providers"
)

// ConfigKeysStatus returns a ProductStatus for p based solely on whether
// the non-secret config keys reported by p.ConfigKeys() are present in cfg.
//
// This is a heuristic stub: it never probes any API. A provider with no
// config keys is always reported as StateActive (no configuration required).
// A provider with non-secret keys is reported as StateConfigured when all
// such keys are present and non-empty, or StateNotConfigured otherwise.
//
// cfg should be the provider-specific config map returned by
// providers.ConfigLoader.LoadProviderConfig for the named provider.
func ConfigKeysStatus(p providers.Provider, cfg map[string]string) ProductStatus {
	keys := p.ConfigKeys()

	// Collect non-secret keys — these are the ones a user must supply.
	var required []string
	for _, k := range keys {
		if !k.Secret {
			required = append(required, k.Name)
		}
	}

	if len(required) == 0 {
		return ProductStatus{
			Product: p.Name(),
			State:   StateActive,
			Details: "no configuration required",
		}
	}

	var missing []string
	for _, name := range required {
		if cfg[name] == "" {
			missing = append(missing, name)
		}
	}

	if len(missing) == 0 {
		return ProductStatus{
			Product: p.Name(),
			State:   StateConfigured,
			Details: "all required config keys are present",
		}
	}

	return ProductStatus{
		Product:   p.Name(),
		State:     StateNotConfigured,
		Details:   "missing config keys: " + strings.Join(missing, ", "),
		SetupHint: "run 'gcx config set' to configure the missing keys",
	}
}
