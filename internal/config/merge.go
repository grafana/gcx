package config

import "maps"

// MergeConfigs deep-merges two configs. Fields in `over` take precedence
// over fields in `base`. Zero-value fields in `over` do not erase `base`.
//
// The keychain fields (keychainStore, keychainFields, keychainPreserve) are
// carried over unchanged from `base`, so any resolved-field tracking on `over`
// is dropped. This is safe: callers resolve the effective current context after
// merging (see LoadLayered), and reconcileKeychain re-derives backing on write
// from the sentinel/plaintext state of each field.
func MergeConfigs(base, over Config) Config {
	result := base

	// Scalar: current-context — higher layer wins if non-empty.
	if over.CurrentContext != "" {
		result.CurrentContext = over.CurrentContext
	}

	// Map: contexts — merge by key.
	if over.Contexts != nil {
		if result.Contexts == nil {
			result.Contexts = make(map[string]*Context)
		}
		for name, overCtx := range over.Contexts {
			if baseCtx, ok := result.Contexts[name]; ok {
				result.Contexts[name] = mergeContexts(baseCtx, overCtx)
			} else {
				result.Contexts[name] = overCtx
			}
		}
	}

	// Top-level cloud: merge environments by key; current-env wins if non-empty.
	if over.Cloud != nil {
		if result.Cloud == nil {
			result.Cloud = over.Cloud
		} else {
			merged := mergeCloudSettings(result.Cloud, over.Cloud)
			result.Cloud = &merged
		}
	}

	// Diagnostics: propagate from any layer that enables it.
	if over.Diagnostics != nil {
		if result.Diagnostics == nil {
			result.Diagnostics = over.Diagnostics
		} else {
			merged := mergeDiagnosticsConfig(result.Diagnostics, over.Diagnostics)
			result.Diagnostics = &merged
		}
	}

	return result
}

func mergeDiagnosticsConfig(base, over *DiagnosticsConfig) DiagnosticsConfig {
	result := *base
	if over.AgentInvocationLog {
		result.AgentInvocationLog = true
	}
	if over.LogDir != "" {
		result.LogDir = over.LogDir
	}
	return result
}

func mergeContexts(base, over *Context) *Context {
	if base == nil {
		return over
	}
	if over == nil {
		return base
	}

	result := *base // shallow copy

	// Grafana config: field-level merge.
	if over.Grafana != nil {
		if result.Grafana == nil {
			result.Grafana = over.Grafana
		} else {
			merged := mergeGrafanaConfig(result.Grafana, over.Grafana)
			result.Grafana = &merged
		}
	}

	// Cloud config: field-level merge.
	if over.Cloud != nil {
		if result.Cloud == nil {
			result.Cloud = over.Cloud
		} else {
			merged := mergeCloudConfig(result.Cloud, over.Cloud)
			result.Cloud = &merged
		}
	}

	// Providers map: merge by key (string→map[string]string).
	if over.Providers != nil {
		if result.Providers == nil {
			result.Providers = make(map[string]map[string]string)
		}
		for k, v := range over.Providers {
			if baseV, ok := result.Providers[k]; ok {
				merged := make(map[string]string, len(baseV)+len(v))
				maps.Copy(merged, baseV)
				maps.Copy(merged, v)
				result.Providers[k] = merged
			} else {
				result.Providers[k] = v
			}
		}
	}

	// Datasources map: merge by key.
	if over.Datasources != nil {
		if result.Datasources == nil {
			result.Datasources = make(map[string]string)
		}
		maps.Copy(result.Datasources, over.Datasources)
	}

	// Named datasource overrides.
	if over.DefaultPrometheusDatasource != "" {
		result.DefaultPrometheusDatasource = over.DefaultPrometheusDatasource
	}
	if over.DefaultLokiDatasource != "" {
		result.DefaultLokiDatasource = over.DefaultLokiDatasource
	}
	if over.DefaultPyroscopeDatasource != "" {
		result.DefaultPyroscopeDatasource = over.DefaultPyroscopeDatasource
	}

	return &result
}

func mergeGrafanaConfig(base, over *GrafanaConfig) GrafanaConfig {
	result := *base
	if over.Server != "" {
		result.Server = over.Server
	}
	if over.User != "" {
		result.User = over.User
	}
	if over.Password != "" {
		result.Password = over.Password
	}
	if over.APIToken != "" {
		result.APIToken = over.APIToken
	}
	if over.OrgID != 0 {
		result.OrgID = over.OrgID
	}
	if over.StackID != 0 {
		result.StackID = over.StackID
	}
	if over.TLS != nil {
		t := *over.TLS
		result.TLS = &t
	}
	if over.OAuthToken != "" {
		result.OAuthToken = over.OAuthToken
	}
	if over.OAuthRefreshToken != "" {
		result.OAuthRefreshToken = over.OAuthRefreshToken
	}
	if over.OAuthTokenExpiresAt != "" {
		result.OAuthTokenExpiresAt = over.OAuthTokenExpiresAt
	}
	if over.OAuthRefreshExpiresAt != "" {
		result.OAuthRefreshExpiresAt = over.OAuthRefreshExpiresAt
	}
	if over.ProxyEndpoint != "" {
		result.ProxyEndpoint = over.ProxyEndpoint
	}
	return result
}

func mergeCloudSettings(base, over *CloudSettings) CloudSettings {
	result := *base
	if over.Current != "" {
		result.Current = over.Current
	}
	if over.Envs != nil {
		if result.Envs == nil {
			result.Envs = make(map[string]*CloudConfig)
		}
		for name, overEnv := range over.Envs {
			if baseEnv, ok := result.Envs[name]; ok {
				merged := mergeCloudConfig(baseEnv, overEnv)
				result.Envs[name] = &merged
			} else {
				result.Envs[name] = overEnv
			}
		}
	}
	return result
}

func mergeCloudConfig(base, over *CloudConfig) CloudConfig {
	result := *base
	if over.Token != "" {
		result.Token = over.Token
	}
	if over.Env != "" {
		result.Env = over.Env
	}
	if over.Stack != "" {
		result.Stack = over.Stack
	}
	if over.OAuthUrl != "" {
		result.OAuthUrl = over.OAuthUrl
	}
	if over.APIUrl != "" {
		result.APIUrl = over.APIUrl
	}
	return result
}
