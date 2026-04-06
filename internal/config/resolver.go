package config

// DefaultDatasourceUID resolves the default datasource UID for the given kind.
//
// Resolution order:
//  1. ctx.Datasources[kind] (new per-kind section) — takes precedence
//  2. Legacy flat field: DefaultPrometheusDatasource / DefaultLokiDatasource / DefaultPyroscopeDatasource
//  3. Returns "" if neither is set; callers are responsible for emitting an error.
func DefaultDatasourceUID(ctx Context, kind string) string {
	if uid := ctx.Datasources[kind]; uid != "" {
		return uid
	}

	switch kind {
	case "prometheus":
		return ctx.DefaultPrometheusDatasource
	case "loki":
		return ctx.DefaultLokiDatasource
	case "pyroscope":
		return ctx.DefaultPyroscopeDatasource
	case "tempo":
		return ctx.DefaultTempoDatasource
	}

	return ""
}
