package query_test

import (
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSharedOptsValidate(t *testing.T) {
	newOpts := func() *dsquery.SharedOpts {
		return &dsquery.SharedOpts{IO: cmdio.Options{OutputFormat: "json"}}
	}

	assertRange := func(t *testing.T, from, to string, want time.Duration) {
		t.Helper()

		parsedFrom, err := time.Parse(time.RFC3339, from)
		require.NoError(t, err)
		parsedTo, err := time.Parse(time.RFC3339, to)
		require.NoError(t, err)

		assert.WithinDuration(t, parsedTo.Add(-want), parsedFrom, time.Second)
	}

	tests := []struct {
		name    string
		setup   func(*dsquery.SharedOpts)
		wantErr string
		assert  func(*testing.T, *dsquery.SharedOpts)
	}{
		{
			name:  "no flags is valid (instant query)",
			setup: func(_ *dsquery.SharedOpts) {},
			assert: func(t *testing.T, opts *dsquery.SharedOpts) {
				t.Helper()
				assert.False(t, opts.IsRange())
			},
		},
		{
			name: "from and to is valid (range query)",
			setup: func(opts *dsquery.SharedOpts) {
				opts.From = "2026-01-01T00:00:00Z"
				opts.To = "2026-01-01T01:00:00Z"
			},
			assert: func(t *testing.T, opts *dsquery.SharedOpts) {
				t.Helper()
				assert.True(t, opts.IsRange())
			},
		},
		{
			name: "from without to is rejected",
			setup: func(opts *dsquery.SharedOpts) {
				opts.From = "2024-01-01T00:00:00Z"
			},
			wantErr: "--to is required when --from is set",
		},
		{
			name: "to without from is rejected",
			setup: func(opts *dsquery.SharedOpts) {
				opts.To = "2024-01-01T00:00:00Z"
			},
			wantErr: "--from is required when --to is set",
		},
		{
			name: "since without to defaults to now",
			setup: func(opts *dsquery.SharedOpts) {
				opts.Since = "1h"
			},
			assert: func(t *testing.T, opts *dsquery.SharedOpts) {
				t.Helper()
				require.NotEmpty(t, opts.From)
				require.NotEmpty(t, opts.To)
				assertRange(t, opts.From, opts.To, time.Hour)
			},
		},
		{
			name: "since with explicit to resolves start relative to end",
			setup: func(opts *dsquery.SharedOpts) {
				opts.Since = "1h"
				opts.To = "2026-03-31T10:00:00Z"
			},
			assert: func(t *testing.T, opts *dsquery.SharedOpts) {
				t.Helper()
				assert.Equal(t, "2026-03-31T09:00:00Z", opts.From)
				assert.Equal(t, "2026-03-31T10:00:00Z", opts.To)
			},
		},
		{
			name: "since and from are mutually exclusive",
			setup: func(opts *dsquery.SharedOpts) {
				opts.Since = "1h"
				opts.From = "now-2h"
			},
			wantErr: "--since is mutually exclusive with --from",
		},
		{
			name: "invalid since duration rejected",
			setup: func(opts *dsquery.SharedOpts) {
				opts.Since = "tomorrow"
			},
			wantErr: "invalid --since duration",
		},
		{
			name: "invalid to time rejected",
			setup: func(opts *dsquery.SharedOpts) {
				opts.Since = "1h"
				opts.To = "later"
			},
			wantErr: "invalid --to time",
		},
		{
			name: "negative since rejected",
			setup: func(opts *dsquery.SharedOpts) {
				opts.Since = "-1h"
			},
			wantErr: "--since must be greater than 0",
		},
		{
			name: "zero since rejected",
			setup: func(opts *dsquery.SharedOpts) {
				opts.Since = "0"
			},
			wantErr: "--since must be greater than 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := newOpts()
			if tt.setup != nil {
				tt.setup(opts)
			}

			err := opts.Validate()
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}

			require.NoError(t, err)
			if tt.assert != nil {
				tt.assert(t, opts)
			}
		})
	}
}

func TestSharedOptsSetup_GraphSupport(t *testing.T) {
	t.Run("graph disabled rejects graph output", func(t *testing.T) {
		opts := &dsquery.SharedOpts{}
		flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
		opts.Setup(flags, false)

		require.NoError(t, flags.Parse([]string{"-o", "graph"}))
		err := opts.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown output format 'graph'")
		assert.Contains(t, err.Error(), "json")
		assert.Contains(t, err.Error(), "table")
		assert.Contains(t, err.Error(), "wide")
		assert.Contains(t, err.Error(), "yaml")
	})

	t.Run("graph enabled accepts graph output", func(t *testing.T) {
		opts := &dsquery.SharedOpts{}
		flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
		opts.Setup(flags, true)

		require.NoError(t, flags.Parse([]string{"-o", "graph"}))
		require.NoError(t, opts.Validate())
	})
}

func TestResolveExpr(t *testing.T) {
	tests := []struct {
		name         string
		flagExpr     string
		args         []string
		exprArgIndex int
		want         string
		wantErr      string
	}{
		{
			name:         "positional arg only",
			args:         []string{"up"},
			exprArgIndex: 0,
			want:         "up",
		},
		{
			name:         "flag only",
			flagExpr:     "up",
			args:         []string{},
			exprArgIndex: 0,
			want:         "up",
		},
		{
			name:         "both provided",
			flagExpr:     "up",
			args:         []string{"up"},
			exprArgIndex: 0,
			wantErr:      "provide the expression as a positional argument or via --expr, not both",
		},
		{
			name:         "neither provided",
			args:         []string{},
			exprArgIndex: 0,
			wantErr:      "expression is required: provide it as a positional argument or via --expr",
		},
		{
			name:         "generic command positional (arg index 1)",
			args:         []string{"uid", "up"},
			exprArgIndex: 1,
			want:         "up",
		},
		{
			name:         "generic command flag (arg index 1 absent)",
			flagExpr:     "up",
			args:         []string{"uid"},
			exprArgIndex: 1,
			want:         "up",
		},
		{
			name:         "generic command both (arg index 1)",
			flagExpr:     "up",
			args:         []string{"uid", "up"},
			exprArgIndex: 1,
			wantErr:      "not both",
		},
		{
			name:         "generic command neither (arg index 1 absent, no flag)",
			args:         []string{"uid"},
			exprArgIndex: 1,
			wantErr:      "expression is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &dsquery.SharedOpts{Expr: tt.flagExpr}
			got, err := opts.ResolveExpr(tt.args, tt.exprArgIndex)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResolveDatasourceFlag(t *testing.T) {
	tests := []struct {
		name      string
		flagValue string
		cfgCtx    *config.Context
		kind      string
		wantUID   string
		wantErr   string
	}{
		{
			name:      "flag value takes precedence",
			flagValue: "explicit-uid",
			cfgCtx:    &config.Context{Datasources: map[string]string{"prometheus": "config-uid"}},
			kind:      "prometheus",
			wantUID:   "explicit-uid",
		},
		{
			name:      "falls back to config",
			flagValue: "",
			cfgCtx:    &config.Context{Datasources: map[string]string{"tempo": "tempo-uid"}},
			kind:      "tempo",
			wantUID:   "tempo-uid",
		},
		{
			name:      "nil config context returns error",
			flagValue: "",
			cfgCtx:    nil,
			kind:      "prometheus",
			wantErr:   "datasource UID is required: use -d flag or set datasources.prometheus in config",
		},
		{
			name:      "empty config returns error",
			flagValue: "",
			cfgCtx:    &config.Context{},
			kind:      "loki",
			wantErr:   "datasource UID is required: use -d flag or set datasources.loki in config",
		},
		{
			name:      "flag value works with nil config",
			flagValue: "my-uid",
			cfgCtx:    nil,
			kind:      "prometheus",
			wantUID:   "my-uid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uid, err := dsquery.ResolveDatasourceFlag(tt.flagValue, tt.cfgCtx, tt.kind)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantUID, uid)
		})
	}
}
