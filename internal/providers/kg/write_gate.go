package kg

import (
	"context"
	"errors"
	"strconv"
)

const (
	writeProviderName  = "kg"
	writeAPIEnabledKey = "write-api-enabled"
)

// writeAPIDisabledMsg is returned verbatim when the write commands are not enabled.
const writeAPIDisabledMsg = "kg write commands are experimental; " +
	"enable with GRAFANA_PROVIDER_KG_WRITE_API_ENABLED=true " +
	"or set providers.kg.write-api-enabled in config"

// requireWriteAPIEnabled returns an error unless providers.kg.write-api-enabled
// is truthy (config file or GRAFANA_PROVIDER_KG_WRITE_API_ENABLED env var).
func requireWriteAPIEnabled(ctx context.Context, loader RESTConfigLoader) error {
	cfg, _, err := loader.LoadProviderConfig(ctx, writeProviderName)
	if err != nil {
		return err
	}
	if enabled, parseErr := strconv.ParseBool(cfg[writeAPIEnabledKey]); parseErr == nil && enabled {
		return nil
	}
	return errors.New(writeAPIDisabledMsg)
}
