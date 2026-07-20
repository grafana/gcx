package config

import (
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
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
	applyCloudEnvOverride(ctx)
	PrepareForEnvParse(ctx)
	if err := parseEnvTags(ctx); err != nil {
		return err
	}
	CleanupAfterEnvParse(ctx)
	if slug, ok := os.LookupEnv("GRAFANA_CLOUD_STACK"); ok {
		ctx.envStackSlug = slug
	}
	return nil
}

// applyCloudEnvOverride synthesizes an ephemeral cloud entry when any
// GRAFANA_CLOUD_* auth variable is set, starting from a copy of the entry the
// context references (if any). The copy keeps env values out of the shared
// named entry, which other contexts reference and Write would persist.
func applyCloudEnvOverride(ctx *Context) {
	_, hasToken := os.LookupEnv("GRAFANA_CLOUD_TOKEN")
	_, hasAPIURL := os.LookupEnv("GRAFANA_CLOUD_API_URL")
	_, hasOAuthURL := os.LookupEnv("GRAFANA_CLOUD_OAUTH_URL")
	if !hasToken && !hasAPIURL && !hasOAuthURL {
		return
	}
	detached := CloudEntry{}
	if ctx.CloudEntry != nil {
		detached = *ctx.CloudEntry
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

		if err := setField(fv, val, envKey); err != nil {
			return err
		}
	}
	return nil
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
