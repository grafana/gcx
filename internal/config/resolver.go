package config

// DefaultDatasourceUID resolves the default datasource UID for the given kind
// from the context's per-kind datasources map. Returns "" if unset; callers
// are responsible for emitting an error.
func DefaultDatasourceUID(ctx Context, kind string) string {
	return ctx.Datasources[kind]
}
