package config

import "github.com/caarlos0/env/v11"

// PrepareForEnvParse initializes nested pointer fields on the Context so that
// env.Parse can populate environment variables like GRAFANA_TLS_CERT_FILE into
// the nested structs. Without this, env.Parse silently skips nil struct pointers.
//
// Call CleanupAfterEnvParse after env.Parse to nil-out any structs that
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
// env.Parse but had no fields actually set. This keeps IsEmpty() and
// nil-pointer checks working correctly downstream.
func CleanupAfterEnvParse(ctx *Context) {
	if ctx.Grafana != nil && ctx.Grafana.TLS != nil && ctx.Grafana.TLS.IsEmpty() {
		ctx.Grafana.TLS = nil
	}
}

// ParseEnvIntoContext is a convenience that combines PrepareForEnvParse,
// env.Parse, and CleanupAfterEnvParse into a single call.
func ParseEnvIntoContext(ctx *Context) error {
	PrepareForEnvParse(ctx)
	if err := env.Parse(ctx); err != nil {
		return err
	}
	CleanupAfterEnvParse(ctx)
	return nil
}
