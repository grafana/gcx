// Package query implements shared infrastructure for datasource query subcommands.
// Each datasource kind (prometheus, loki, pyroscope, tempo, generic) exposes an
// exported constructor that returns a `query` cobra.Command to be registered
// under its parent kind command (e.g., `datasources prometheus query`).
package query

import (
	"context"
	"errors"
	"fmt"
	"time"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	internalconfig "github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/datasources"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/pflag"
)

// sharedQueryOpts holds flags shared across all typed query subcommands.
type sharedQueryOpts struct {
	IO    cmdio.Options
	From  string
	To    string
	Step  string
	Since string
}

func (opts *sharedQueryOpts) setup(flags *pflag.FlagSet) {
	registerCodecs(&opts.IO)
	opts.IO.BindFlags(flags)

	flags.StringVar(&opts.From, "from", "", "Start time (RFC3339, Unix timestamp, or relative like 'now-1h')")
	flags.StringVar(&opts.To, "to", "", "End time (RFC3339, Unix timestamp, or relative like 'now')")
	flags.StringVar(&opts.Step, "step", "", "Query step (e.g., '15s', '1m')")
	flags.StringVar(&opts.Since, "since", "", "Duration before --to (or now if omitted); mutually exclusive with --from")
}

// Validate validates shared flags and resolves --since into From/To.
func (opts *sharedQueryOpts) Validate() error {
	if err := opts.IO.Validate(); err != nil {
		return err
	}

	if opts.Since == "" {
		return nil
	}

	if opts.From != "" {
		return errors.New("--since is mutually exclusive with --from")
	}

	d, err := ParseDuration(opts.Since)
	if err != nil {
		return fmt.Errorf("invalid --since duration: %w", err)
	}
	if d <= 0 {
		return errors.New("--since must be greater than 0")
	}

	now := time.Now()
	end, err := ParseTime(opts.To, now)
	if err != nil {
		return fmt.Errorf("invalid --to time: %w", err)
	}
	if end.IsZero() {
		end = now
	}
	opts.From = end.Add(-d).Format(time.RFC3339)
	opts.To = end.Format(time.RFC3339)

	return nil
}

// parseTimes parses From/To/Step into time.Time and time.Duration values.
func (opts *sharedQueryOpts) parseTimes(now time.Time) (time.Time, time.Time, time.Duration, error) {
	start, err := ParseTime(opts.From, now)
	if err != nil {
		return time.Time{}, time.Time{}, 0, fmt.Errorf("invalid --from time: %w", err)
	}

	end, err := ParseTime(opts.To, now)
	if err != nil {
		return time.Time{}, time.Time{}, 0, fmt.Errorf("invalid --to time: %w", err)
	}

	step, err := ParseDuration(opts.Step)
	if err != nil {
		return time.Time{}, time.Time{}, 0, fmt.Errorf("invalid --step duration: %w", err)
	}

	return start, end, step, nil
}

// resolveTypedArgs parses positional args for typed subcommands.
// Typed subcommands accept: [DATASOURCE_UID] EXPR
// If only one arg is provided, it is EXPR and DATASOURCE_UID is resolved from config.
// If two args are provided, arg[0] is DATASOURCE_UID and arg[1] is EXPR.
func resolveTypedArgs(args []string, configOpts *cmdconfig.Options, ctx context.Context, kind string) (string, string, error) {
	switch len(args) {
	case 0:
		return "", "", errors.New("EXPR is required")
	case 1:
		// No UID provided — try default from config.
		fullCfg, cfgErr := configOpts.LoadConfig(ctx)
		if cfgErr != nil {
			return "", "", cfgErr
		}
		uid := internalconfig.DefaultDatasourceUID(*fullCfg.GetCurrentContext(), kind)
		if uid == "" {
			return "", "", fmt.Errorf("DATASOURCE_UID is required: provide it as the first positional argument or configure datasources.%s in your context", kind)
		}
		return uid, args[0], nil
	case 2:
		return args[0], args[1], nil
	default:
		return "", "", errors.New("too many arguments: expected [DATASOURCE_UID] EXPR")
	}
}

// getDatasourceType fetches the datasource type string from the Grafana API.
func getDatasourceType(ctx context.Context, configOpts *cmdconfig.Options, datasourceUID string) (string, error) {
	restCfg, err := configOpts.LoadGrafanaConfig(ctx)
	if err != nil {
		return "", err
	}

	dsClient, err := datasources.NewClient(restCfg)
	if err != nil {
		return "", fmt.Errorf("failed to create datasource client: %w", err)
	}

	ds, err := dsClient.GetByUID(ctx, datasourceUID)
	if err != nil {
		return "", fmt.Errorf("failed to get datasource %q: %w", datasourceUID, err)
	}

	return ds.Type, nil
}

// normalizeKind converts a Grafana datasource plugin ID to its short kind name.
// Some plugins use the short name directly (e.g., "prometheus"), while others
// use a longer ID (e.g., "grafana-pyroscope-datasource").
// If the plugin ID is not recognized, it is returned as-is.
func normalizeKind(pluginID string) string {
	switch pluginID {
	case "prometheus", "loki", "tempo":
		return pluginID
	case "grafana-pyroscope-datasource":
		return "pyroscope"
	default:
		return pluginID
	}
}

// validateDatasourceType checks that the datasource's actual type matches the expected kind.
func validateDatasourceType(ctx context.Context, configOpts *cmdconfig.Options, datasourceUID, expectedKind string) error {
	dsType, err := getDatasourceType(ctx, configOpts, datasourceUID)
	if err != nil {
		return err
	}

	if normalizeKind(dsType) != expectedKind {
		return fmt.Errorf("datasource %s is type %s, not %s", datasourceUID, dsType, expectedKind)
	}

	return nil
}
