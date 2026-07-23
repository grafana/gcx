package config

import "maps"

// MergeConfigs deep-merges two configs. Fields in `over` take precedence
// over fields in `base`. Zero-value fields in `over` do not erase `base`.
//
// Runtime credential state is retained only for the atomic stack/cloud entries
// that win the merge, including each entry's defining source identity.
func MergeConfigs(base, over Config) Config {
	result := base
	result.migrationDeferred = base.migrationDeferred || over.migrationDeferred

	if over.Version > result.Version {
		result.Version = over.Version
	}

	// Scalar: current-context — higher layer wins if non-empty.
	if over.CurrentContext != "" {
		result.CurrentContext = over.CurrentContext
	}

	// Maps: stacks and cloud entries are ATOMIC across layers — a same-named
	// entry in a higher layer replaces the lower layer's entry wholesale,
	// never field-by-field. This guarantees a credential and its destination
	// (server, api-url) always come from the same file: a repo-local layer
	// cannot graft its own destination onto an entry whose token lives in the
	// user config. A hostile layer can only shadow an entry (breaking it),
	// not combine with it.
	if over.Stacks != nil {
		if result.Stacks == nil {
			result.Stacks = make(map[string]*StackConfig)
		}
		maps.Copy(result.Stacks, over.Stacks)
	}

	if over.Cloud != nil {
		if result.Cloud == nil {
			result.Cloud = make(map[string]*CloudEntry)
		}
		maps.Copy(result.Cloud, over.Cloud)
	}

	// Global resources: last definition wins (see mergeResourcesConfig).
	if over.Resources != nil {
		if result.Resources == nil {
			result.Resources = over.Resources
		} else {
			merged := mergeResourcesConfig(result.Resources, over.Resources)
			result.Resources = &merged
		}
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

	// Diagnostics: merged per field — bools propagate from any layer that
	// enables them, strings are last-non-empty-wins (see mergeDiagnosticsConfig).
	if over.Diagnostics != nil {
		if result.Diagnostics == nil {
			result.Diagnostics = over.Diagnostics
		} else {
			merged := mergeDiagnosticsConfig(result.Diagnostics, over.Diagnostics)
			result.Diagnostics = &merged
		}
	}

	// Re-wire resolved views: merged contexts may reference stacks or cloud
	// entries contributed by either layer.
	result.Resolve()
	mergeKeychainRuntime(&result, base, over)

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
	if over.Telemetry != "" {
		result.Telemetry = over.Telemetry
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

	if over.Stack != "" {
		result.Stack = over.Stack
	}
	if over.Cloud != "" {
		result.Cloud = over.Cloud
	}

	// Datasources map: merge by key.
	if over.Datasources != nil {
		if result.Datasources == nil {
			result.Datasources = make(map[string]string)
		}
		maps.Copy(result.Datasources, over.Datasources)
	}

	return &result
}

func mergeResourcesConfig(base, over *ResourcesConfig) ResourcesConfig {
	result := *base
	if over.AssumeServerDryRun != nil {
		result.AssumeServerDryRun = over.AssumeServerDryRun
	}
	return result
}
