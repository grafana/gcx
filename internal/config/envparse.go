package config

import (
	"fmt"
	"maps"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/credentials"
)

// PrepareForEnvParse initializes nested pointer fields on the Context so that
// parseEnvTags can populate environment variables like GRAFANA_TLS_CERT_FILE into
// the nested structs. Without this, nil struct pointers are silently skipped.
//
// Call CleanupAfterEnvParse after parsing to nil-out any structs that
// remained empty (preserving IsEmpty semantics).
func PrepareForEnvParse(ctx *Context) {
	if ctx.Grafana == nil {
		ctx.Grafana = &GrafanaConfig{}
	}
	if ctx.Grafana.TLS == nil {
		ctx.Grafana.TLS = &TLS{}
	}
}

// CleanupAfterEnvParse nils out nested structs that were only initialized for
// env parsing but had no fields actually set. This keeps IsEmpty() and
// nil-pointer checks working correctly downstream.
func CleanupAfterEnvParse(ctx *Context) {
	if ctx.Grafana != nil && ctx.Grafana.TLS != nil && ctx.Grafana.TLS.IsEmpty() {
		ctx.Grafana.TLS = nil
	}
}

// ParseEnvIntoContext is a convenience that combines the cloud-entry env
// override, PrepareForEnvParse, parseEnvTags, and CleanupAfterEnvParse into a
// single call.
func ParseEnvIntoContext(ctx *Context) error {
	detachStackRuntimeView(ctx)
	ctx.runtimeSecretOverrides = map[credentials.Field]bool{}
	for envKey, field := range map[string]credentials.Field{
		"GRAFANA_TOKEN":                   credentials.FieldGrafanaToken,
		"GRAFANA_PASSWORD":                credentials.FieldGrafanaPassword,
		"GRAFANA_CLOUD_TOKEN":             credentials.FieldCloudToken,
		"GRAFANA_PROVIDER_SYNTH_SM_TOKEN": credentials.FieldSMToken,
	} {
		if value, ok := os.LookupEnv(envKey); ok && !IsBlankCredentialEnvironmentOverride(envKey, value) {
			ctx.runtimeSecretOverrides[field] = true
		}
	}
	applyCloudEnvOverride(ctx)
	PrepareForEnvParse(ctx)
	if err := parseEnvTags(ctx); err != nil {
		return err
	}
	CleanupAfterEnvParse(ctx)
	if ctx.StackEntry != nil {
		// PrepareForEnvParse may have created Grafana for a named stack that had
		// no persisted Grafana block. Keep binding checks on the detached stack
		// pointed at the effective runtime view.
		ctx.StackEntry.Grafana = ctx.Grafana
		ctx.StackEntry.Providers = ctx.Providers
	}
	if slug, ok := os.LookupEnv("GRAFANA_CLOUD_STACK"); ok {
		ctx.envStackSlug = slug
	}
	return nil
}

// detachStackRuntimeView makes the selected context safe for process-local
// overrides. Config.Resolve deliberately wires contexts that name the same
// stack to shared pointers; mutating those pointers here would make an env
// override for one context visible through every sibling context and through
// Config.Stacks. Keep the stack's immutable identity and rejection evidence so
// credential binding checks still use the persisted owner, but deep-clone every
// nested value that runtime consumers may mutate.
func detachStackRuntimeView(ctx *Context) {
	if ctx == nil {
		return
	}

	grafana := cloneRuntimeGrafana(ctx.Grafana)
	providers := cloneRuntimeProviders(ctx.Providers)

	if ctx.StackEntry != nil {
		if providers == nil {
			// Provider environment overlays run after this function and allocate
			// nested maps through Context.Providers. Give the context and its
			// detached stack one shared runtime-only root map so those values also
			// participate in credential binding checks.
			providers = map[string]map[string]string{}
		}
		detached := *ctx.StackEntry
		detached.credentialRejections = maps.Clone(ctx.StackEntry.credentialRejections)
		detached.Grafana = grafana
		detached.Providers = providers
		if ctx.StackEntry.Resources != nil {
			resources := *ctx.StackEntry.Resources
			resources.AssumeServerDryRun = slices.Clone(ctx.StackEntry.Resources.AssumeServerDryRun)
			detached.Resources = &resources
		}
		ctx.StackEntry = &detached
	}

	ctx.Grafana = grafana
	ctx.Providers = providers
}

func cloneRuntimeGrafana(source *GrafanaConfig) *GrafanaConfig {
	if source == nil {
		return nil
	}
	detached := *source
	detached.TLS = source.TLS.clone()
	return &detached
}

func cloneRuntimeProviders(source map[string]map[string]string) map[string]map[string]string {
	if source == nil {
		return nil
	}
	detached := make(map[string]map[string]string, len(source))
	for provider, values := range source {
		detached[provider] = maps.Clone(values)
	}
	return detached
}

// applyCloudEnvOverride synthesizes an ephemeral cloud entry when any
// GRAFANA_CLOUD_* auth variable is set, starting from a copy of the entry the
// context references (if any). The copy keeps env values out of the shared
// named entry, which other contexts reference and Write would persist.
func applyCloudEnvOverride(ctx *Context) {
	token, hasToken := os.LookupEnv("GRAFANA_CLOUD_TOKEN")
	hasToken = hasToken && !IsBlankCredentialEnvironmentOverride("GRAFANA_CLOUD_TOKEN", token)
	_, hasAPIURL := os.LookupEnv("GRAFANA_CLOUD_API_URL")
	_, hasOAuthURL := os.LookupEnv("GRAFANA_CLOUD_OAUTH_URL")
	if !hasToken && !hasAPIURL && !hasOAuthURL {
		return
	}
	detached := CloudEntry{}
	if ctx.CloudEntry != nil {
		detached = *ctx.CloudEntry
		detached.credentialRejections = maps.Clone(ctx.CloudEntry.credentialRejections)
	}
	// parseEnvTags fills the env-tagged fields on the detached copy.
	ctx.CloudEntry = &detached
}

// parseEnvTags walks the struct fields of v (which must be a pointer to a struct)
// and populates fields that have an `env` struct tag from the corresponding
// environment variable. Nested struct pointers are followed if non-nil.
// Supported field types: string, bool, int64.
func parseEnvTags(v any) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("parseEnvTags: expected pointer to struct, got %T", v)
	}
	return walkStruct(rv.Elem())
}

func walkStruct(sv reflect.Value) error {
	st := sv.Type()
	for i := range st.NumField() {
		field := st.Field(i)
		fv := sv.Field(i)

		// Follow non-nil struct pointers into nested structs.
		if field.Type.Kind() == reflect.Pointer && field.Type.Elem().Kind() == reflect.Struct {
			if !fv.IsNil() {
				if err := walkStruct(fv.Elem()); err != nil {
					return err
				}
			}
			continue
		}

		envKey := field.Tag.Get("env")
		if envKey == "" {
			continue
		}

		// Strip options after comma (e.g. env:"FOO,required").
		if idx := strings.IndexByte(envKey, ','); idx >= 0 {
			envKey = envKey[:idx]
		}

		val, ok := os.LookupEnv(envKey)
		if !ok {
			continue
		}
		// Empty credential variables are conventionally used to clear inherited
		// process state in CI. They are not a fresh credential and must not erase a
		// stored value or authorize re-binding it to an overridden destination.
		if IsBlankCredentialEnvironmentOverride(envKey, val) {
			continue
		}

		if err := setField(fv, val, envKey); err != nil {
			return err
		}
	}
	return nil
}

// IsBlankCredentialEnvironmentOverride reports whether an environment value
// is blank and its variable is a credential input. Blank credentials are often
// used to clear inherited process state in CI; they must not erase a stored
// credential or authorize binding that credential to another destination.
// Non-credential variables retain their existing blank-value semantics.
func IsBlankCredentialEnvironmentOverride(envKey, value string) bool {
	return isCredentialEnvironmentVariable(envKey) && strings.TrimSpace(value) == ""
}

func isCredentialEnvironmentVariable(envKey string) bool {
	switch envKey {
	case "GRAFANA_TOKEN", "GRAFANA_PASSWORD", "GRAFANA_CLOUD_TOKEN", "GRAFANA_PROVIDER_SYNTH_SM_TOKEN":
		return true
	default:
		return false
	}
}

func setField(fv reflect.Value, val, envKey string) error {
	switch fv.Kind() {
	case reflect.String:
		fv.SetString(val)
	case reflect.Bool:
		if val == "" {
			return nil
		}
		b, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("env %s: %w", envKey, err)
		}
		fv.SetBool(b)
	case reflect.Int64:
		if val == "" {
			return nil
		}
		n, err := strconv.ParseInt(val, 10, 64)
		if err != nil {
			return fmt.Errorf("env %s: %w", envKey, err)
		}
		fv.SetInt(n)
	default:
		return fmt.Errorf("env %s: unsupported field type %s", envKey, fv.Type())
	}
	return nil
}
