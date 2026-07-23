package root

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// parseFailureTestTree builds gcx-shaped commands with flags:
// root (--context string, --agent bool) > dashboards > {search, get (--format)},
// plus completion > zsh and version.
func parseFailureTestTree() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:         "gcx",
		Annotations: map[string]string{cobra.CommandDisplayNameAnnotation: "gcx"},
	}
	rootCmd.PersistentFlags().String("context", "", "")
	rootCmd.PersistentFlags().Bool("agent", false, "")

	dashboards := &cobra.Command{Use: "dashboards"}
	search := &cobra.Command{Use: "search", Run: func(*cobra.Command, []string) {}}
	get := &cobra.Command{Use: "get", Run: func(*cobra.Command, []string) {}}
	get.Flags().String("format", "", "")
	dashboards.AddCommand(search, get)
	rootCmd.AddCommand(dashboards)

	completion := &cobra.Command{Use: "completion"}
	completion.AddCommand(&cobra.Command{Use: "zsh", Run: func(*cobra.Command, []string) {}})
	rootCmd.AddCommand(completion)
	rootCmd.AddCommand(&cobra.Command{Use: "version", Run: func(*cobra.Command, []string) {}})

	return rootCmd
}

// TestParseFailureTelemetryInfo_GoldenPII is the golden PII test required by
// the usage-stats design: it pins down exactly what leaves the process for
// each parse failure shape - truncation of attempted_command, the
// command-shape filter, and distance preservation for redacted tokens.
func TestParseFailureTelemetryInfo_GoldenPII(t *testing.T) {
	// Point config discovery at an empty dir so a real user config (whose
	// context names are redacted) can never change what these cases emit.
	t.Setenv(config.ConfigFileEnvVar, filepath.Join(t.TempDir(), "no-config.yaml"))

	tests := []struct {
		name     string
		args     []string
		suppress bool
		want     ParseFailure
	}{
		{
			name: "typo at root",
			args: []string{"dashbords"},
			want: ParseFailure{
				Kind: parseErrorUnknownCommand, Parent: "", Token: "dashbords",
				Attempted: "dashbords", Nearest: "dashboards", Distance: 1,
			},
		},
		{
			name: "typo under a group",
			args: []string{"dashboards", "serch"},
			want: ParseFailure{
				Kind: parseErrorUnknownCommand, Parent: "dashboards", Token: "serch",
				Attempted: "dashboards serch", Nearest: "search", Distance: 1,
			},
		},
		{
			name: "novel top-level guess",
			args: []string{"deploy"},
			want: ParseFailure{
				Kind: parseErrorUnknownCommand, Parent: "", Token: "deploy",
				Attempted: "deploy", Nearest: "", Distance: -1,
			},
		},
		{
			name: "attempted command truncated at the unknown token",
			args: []string{"dashboards", "serch", "my-secret-dashboard", "--name", "hunter2"},
			want: ParseFailure{
				Kind: parseErrorUnknownCommand, Parent: "dashboards", Token: "serch",
				Attempted: "dashboards serch", Nearest: "search", Distance: 1,
			},
		},
		{
			name: "flag value never mistaken for the token",
			args: []string{"--context", "prod-stack", "dashbords"},
			want: ParseFailure{
				Kind: parseErrorUnknownCommand, Parent: "", Token: "dashbords",
				Attempted: "dashbords", Nearest: "dashboards", Distance: 1,
			},
		},
		{
			name: "unknown flag value consumed conservatively",
			args: []string{"--tokn", "secret-value", "dashboards"},
			want: ParseFailure{
				Kind: parseErrorInvalidArgs, Parent: "dashboards",
				Attempted: "dashboards", Distance: -1,
			},
		},
		{
			name: "uuid token redacted",
			args: []string{"4f3a2b1c-9d0e-4a7b-8c6d-1e2f3a4b5c6d"},
			want: ParseFailure{
				Kind: parseErrorUnknownCommand, Parent: "", Token: redactedToken,
				Attempted: redactedToken, Nearest: "", Distance: -1,
			},
		},
		{
			name: "url token redacted",
			args: []string{"https://user:pass@example.com/secret"},
			want: ParseFailure{
				Kind: parseErrorUnknownCommand, Parent: "", Token: redactedToken,
				Attempted: redactedToken, Nearest: "", Distance: -1,
			},
		},
		{
			name: "secret-looking token redacted",
			args: []string{"MyToken123"},
			want: ParseFailure{
				Kind: parseErrorUnknownCommand, Parent: "", Token: redactedToken,
				Attempted: redactedToken, Nearest: "", Distance: -1,
			},
		},
		{
			name: "distance preserved when the token is redacted",
			args: []string{"dashboards", "Serch"},
			want: ParseFailure{
				Kind: parseErrorUnknownCommand, Parent: "dashboards", Token: redactedToken,
				Attempted: "dashboards " + redactedToken, Nearest: "search", Distance: 1,
			},
		},
		{
			name: "positionals on a runnable command are values, not tokens",
			args: []string{"dashboards", "get", "my-secret-dashboard", "extra"},
			want: ParseFailure{
				Kind: parseErrorInvalidArgs, Parent: "dashboards get",
				Attempted: "dashboards get", Distance: -1,
			},
		},
		{
			name: "double dash ends command traversal",
			args: []string{"dashboards", "--", "hunter2"},
			want: ParseFailure{
				Kind: parseErrorInvalidArgs, Parent: "dashboards",
				Attempted: "dashboards", Distance: -1,
			},
		},
		{
			name:     "parse errors under completion stay suppressed",
			args:     []string{"completion", "bogus"},
			suppress: true,
		},
		{
			name:     "parse errors under version stay suppressed",
			args:     []string{"version", "extra"},
			suppress: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := parseFailureTelemetryInfo(parseFailureTestTree(), tt.args, nil)

			if tt.suppress {
				assert.True(t, info.Suppress)
				return
			}
			require.NotNil(t, info.ParseError)
			assert.Equal(t, tt.want, *info.ParseError)
			assert.Equal(t, tt.want.Parent, info.Command)

			// Nothing that looks like a value may survive into any field.
			serialized := fmt.Sprintf("%+v", *info.ParseError)
			for _, secret := range []string{"hunter2", "my-secret-dashboard", "prod-stack", "secret-value", "MyToken123", "4f3a2b1c", "example.com"} {
				assert.NotContains(t, serialized, secret)
			}
		})
	}
}

// A token naming one of the user's own contexts or Cloud stacks is
// command-shaped but identifying - it reaches command position when arguments
// are misplaced - so it must be redacted even though the shape filter passes
// it.
func TestParseFailureRedactsConfiguredNames(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	cfg := "contexts:\n  acme-corp:\n    cloud:\n      stack: acme-prod\ncurrent-context: acme-corp\n"
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfg), 0o600))
	t.Setenv(config.ConfigFileEnvVar, cfgPath)

	for _, token := range []string{"acme-corp", "acme-prod"} {
		info := parseFailureTelemetryInfo(parseFailureTestTree(), []string{token, "dashboards"}, nil)
		require.NotNil(t, info.ParseError, token)
		assert.Equal(t, redactedToken, info.ParseError.Token, token)
		assert.NotContains(t, fmt.Sprintf("%+v", *info.ParseError), token)
	}

	// An unrelated guess with the same shape still comes through raw.
	info := parseFailureTelemetryInfo(parseFailureTestTree(), []string{"deploy"}, nil)
	require.NotNil(t, info.ParseError)
	assert.Equal(t, "deploy", info.ParseError.Token)
}

func TestFlagFailureTelemetryInfo(t *testing.T) {
	rootCmd := parseFailureTestTree()
	dashboards, _, err := rootCmd.Find([]string{"dashboards"})
	require.NoError(t, err)
	get, _, err := rootCmd.Find([]string{"dashboards", "get"})
	require.NoError(t, err)
	version, _, err := rootCmd.Find([]string{"version"})
	require.NoError(t, err)

	tests := []struct {
		name     string
		cmd      *cobra.Command
		err      error
		suppress bool
		want     ParseFailure
	}{
		{
			name: "unknown flag with a near match",
			cmd:  get,
			err:  errors.New("unknown flag: --frmat"),
			want: ParseFailure{
				Kind: parseErrorUnknownFlag, Parent: "dashboards get",
				Attempted: "dashboards get", Flags: "frmat", Nearest: "format", Distance: 1,
			},
		},
		{
			name: "unknown shorthand flag",
			cmd:  dashboards,
			err:  errors.New("unknown shorthand flag: 'q' in -qv"),
			want: ParseFailure{
				Kind: parseErrorUnknownFlag, Parent: "dashboards",
				Attempted: "dashboards", Flags: "q", Nearest: "", Distance: -1,
			},
		},
		{
			name: "unknown flag name failing the shape filter is redacted",
			cmd:  dashboards,
			err:  errors.New("unknown flag: --https://example.com/secret"),
			want: ParseFailure{
				Kind: parseErrorUnknownFlag, Parent: "dashboards",
				Attempted: "dashboards", Flags: redactedToken, Nearest: "", Distance: -1,
			},
		},
		{
			name: "flag errors embedding values record nothing from the message",
			cmd:  get,
			err:  errors.New(`invalid argument "hunter2" for "--format" flag: bad value`),
			want: ParseFailure{
				Kind: parseErrorInvalidArgs, Parent: "dashboards get",
				Attempted: "dashboards get", Distance: -1,
			},
		},
		{
			name:     "flag failures on suppressed commands stay suppressed",
			cmd:      version,
			err:      errors.New("unknown flag: --bogus"),
			suppress: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := flagFailureTelemetryInfo(&flagParseError{cmd: tt.cmd, err: tt.err})

			if tt.suppress {
				assert.True(t, info.Suppress)
				return
			}
			require.NotNil(t, info.ParseError)
			assert.Equal(t, tt.want, *info.ParseError)
			serialized := fmt.Sprintf("%+v", *info.ParseError)
			assert.NotContains(t, serialized, "hunter2")
			assert.NotContains(t, serialized, "example.com")
		})
	}
}

func TestFilterCommandToken(t *testing.T) {
	tests := []struct {
		token string
		want  string
	}{
		// Command-shaped tokens pass through.
		{"serch", "serch"},
		{"deploy", "deploy"},
		{"logs-tail", "logs-tail"},
		{"instrumentation", "instrumentation"},
		{"a", "a"},
		// Shape violations are redacted.
		{"Dashboards", redactedToken},                           // uppercase
		{"my_secret", redactedToken},                            // underscore
		{"abc123", redactedToken},                               // digits
		{"a-b-c", redactedToken},                                // more than one hyphen
		{"-dash", redactedToken},                                // leading hyphen
		{"", redactedToken},                                     // empty
		{"supercalifragilistical", redactedToken},               // longer than 20
		{"some-uuid-looking-token-4f3a2b1c", redactedToken},     // digits, hyphens, length
		{"4f3a2b1c-9d0e-4a7b-8c6d-1e2f3a4b5c6d", redactedToken}, // UUID
		{"https://example.com", redactedToken},                  // URL
		{"10.0.0.1", redactedToken},                             // IP
		{"qwmzkxvbjfhdpl", redactedToken},                       // high entropy: 14 distinct chars
	}

	for _, tt := range tests {
		t.Run(tt.token, func(t *testing.T) {
			assert.Equal(t, tt.want, filterCommandToken(tt.token))
		})
	}
}

func TestUnknownFlagName(t *testing.T) {
	tests := []struct {
		msg  string
		want string
		ok   bool
	}{
		{"unknown flag: --frmat", "frmat", true},
		{"unknown shorthand flag: 'q' in -qv", "q", true},
		{"flag needs an argument: --output", "", false},
		{`invalid argument "secret" for "--limit" flag: parse error`, "", false},
		{"flag has been renamed; use --insecure-log-http-payload instead", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.msg, func(t *testing.T) {
			name, ok := unknownFlagName(errors.New(tt.msg))
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, name)
		})
	}
}

func TestLevenshtein(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"serch", "search", 1},
		{"dashbords", "dashboards", 1},
		{"resourcse", "resources", 2},
		{"deploy", "dashboards", 8},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.want, levenshtein(tt.a, tt.b), "%s vs %s", tt.a, tt.b)
	}
}
