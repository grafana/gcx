package resources_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/cmd/gcx/resources"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/spf13/pflag"
)

// assertUsageErrorExitCode pins the DESIGN.md exit-code taxonomy for the new
// pull/edit rejections: they are usage errors (bad flag combinations), so the
// error must classify through cmd/gcx/fail as exit 2, not the generic exit 1.
func assertUsageErrorExitCode(t *testing.T, err error) {
	t.Helper()
	usageErr := &fail.UsageError{}
	if !errors.As(err, &usageErr) {
		t.Fatalf("Validate() error = %T, want *fail.UsageError", err)
	}
	detailed := fail.ErrorToDetailedError(err)
	if detailed.ExitCode == nil {
		t.Fatal("converted DetailedError has no ExitCode, want 2 (usage error)")
	}
	if *detailed.ExitCode != gcxerrors.ExitUsageError {
		t.Fatalf("converted exit code = %d, want %d (usage error)", *detailed.ExitCode, gcxerrors.ExitUsageError)
	}
}

// The pull and edit commands use OutputFormat as the on-disk file extension,
// the encoder, and (for edit) the round-trip decode format. Their default must
// therefore stay pinned to json in agent mode — the agents display codec would
// write `<name>.agents` files containing spill-summary envelopes instead of
// resource content. Regression tests for #387 Track B.

func TestPullDefaultFormatInAgentMode(t *testing.T) {
	tests := []struct {
		name           string
		agentMode      bool
		explicitOutput string // simulates -o flag; empty = use default
		wantFormat     string
	}{
		{
			name:       "agent mode default stays json",
			agentMode:  true,
			wantFormat: "json",
		},
		{
			name:       "non-agent mode default is json",
			agentMode:  false,
			wantFormat: "json",
		},
		{
			name:           "explicit -o yaml still wins in agent mode",
			agentMode:      true,
			explicitOutput: "yaml",
			wantFormat:     "yaml",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			agent.SetFlag(tc.agentMode)
			t.Cleanup(func() { agent.SetFlag(false) })

			flags := pflag.NewFlagSet("pull", pflag.ContinueOnError)
			opts := resources.NewPullOptsForTest(flags)

			if tc.explicitOutput != "" {
				if err := flags.Set("output", tc.explicitOutput); err != nil {
					t.Fatalf("set -o %s: %v", tc.explicitOutput, err)
				}
			}

			if opts.IO.OutputFormat != tc.wantFormat {
				t.Fatalf("pull output format = %q, want %q", opts.IO.OutputFormat, tc.wantFormat)
			}
			if err := opts.Validate(); err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
		})
	}
}

func TestEditDefaultFormatInAgentMode(t *testing.T) {
	agent.SetFlag(true)
	t.Cleanup(func() { agent.SetFlag(false) })

	flags := pflag.NewFlagSet("edit", pflag.ContinueOnError)
	opts := resources.NewEditOptsForTest(flags)

	if opts.IO.OutputFormat != "json" {
		t.Fatalf("edit output format in agent mode = %q, want %q", opts.IO.OutputFormat, "json")
	}
	if err := opts.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestPullAndEditRejectAgentsOutputFormat(t *testing.T) {
	agent.SetFlag(false)
	t.Cleanup(func() { agent.SetFlag(false) })

	tests := []struct {
		name     string
		validate func(t *testing.T) error
	}{
		{
			name: "pull rejects -o agents",
			validate: func(t *testing.T) error {
				t.Helper()
				flags := pflag.NewFlagSet("pull", pflag.ContinueOnError)
				opts := resources.NewPullOptsForTest(flags)
				if err := flags.Set("output", "agents"); err != nil {
					t.Fatalf("set -o agents: %v", err)
				}
				return opts.Validate()
			},
		},
		{
			name: "edit rejects -o agents",
			validate: func(t *testing.T) error {
				t.Helper()
				flags := pflag.NewFlagSet("edit", pflag.ContinueOnError)
				opts := resources.NewEditOptsForTest(flags)
				if err := flags.Set("output", "agents"); err != nil {
					t.Fatalf("set -o agents: %v", err)
				}
				return opts.Validate()
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.validate(t)
			if err == nil {
				t.Fatal("Validate() = nil, want agents rejection error")
			}
			if !strings.Contains(err.Error(), "agents") {
				t.Fatalf("Validate() error = %q, want mention of 'agents'", err.Error())
			}
			assertUsageErrorExitCode(t, err)
		})
	}
}

// TestPullAndEditRejectJSONAndJQ: --json (field selection/discovery) and
// --jq (transformation) shape the encoded document, but both commands
// round-trip that document as the resource — edit decodes the editor buffer
// back, and pull writes resource files that push reads back. A
// field-selected or jq-transformed document is not the resource, so both
// flags must fail validation upfront, before any fetch or editor launch.
// For pull the flags were previously advertised but silently ignored (it
// encodes via opts.IO.Codec() directly).
func TestPullAndEditRejectJSONAndJQ(t *testing.T) {
	agent.SetFlag(false)
	t.Cleanup(func() { agent.SetFlag(false) })

	validators := map[string]func(flags *pflag.FlagSet) func() error{
		"pull": func(flags *pflag.FlagSet) func() error {
			return resources.NewPullOptsForTest(flags).Validate
		},
		"edit": func(flags *pflag.FlagSet) func() error {
			return resources.NewEditOptsForTest(flags).Validate
		},
	}

	flagCases := []struct {
		name  string
		flag  string
		value string
	}{
		{name: "rejects --json field selection", flag: "json", value: "metadata.name"},
		{name: "rejects --json discovery", flag: "json", value: "list"},
		{name: "rejects --jq", flag: "jq", value: ".metadata"},
	}

	for cmdName, newValidate := range validators {
		for _, tc := range flagCases {
			t.Run(cmdName+" "+tc.name, func(t *testing.T) {
				flags := pflag.NewFlagSet(cmdName, pflag.ContinueOnError)
				validate := newValidate(flags)
				if err := flags.Set(tc.flag, tc.value); err != nil {
					t.Fatalf("set --%s: %v", tc.flag, err)
				}

				err := validate()
				if err == nil {
					t.Fatalf("Validate() = nil, want --%s rejection error", tc.flag)
				}
				if !strings.Contains(err.Error(), "--"+tc.flag) {
					t.Fatalf("Validate() error = %q, want mention of --%s", err.Error(), tc.flag)
				}
				if !strings.Contains(err.Error(), "round-trip") {
					t.Fatalf("Validate() error = %q, want round-trip rationale", err.Error())
				}
				assertUsageErrorExitCode(t, err)
			})
		}
	}
}

// TestPullAndEditRejectionOrdering: the typed exit-2 rejections must fire
// before the shared IO.Validate, whose errors are untyped (exit 1 today,
// repo-wide). A mixed invocation like `pull -o yaml --json x` must classify
// exactly like a solo `--json x` — this was the known exit-code ordering
// defect in the original #1032 extraction.
func TestPullAndEditRejectionOrdering(t *testing.T) {
	agent.SetFlag(false)
	t.Cleanup(func() { agent.SetFlag(false) })

	validators := map[string]func(flags *pflag.FlagSet) func() error{
		"pull": func(flags *pflag.FlagSet) func() error {
			return resources.NewPullOptsForTest(flags).Validate
		},
		"edit": func(flags *pflag.FlagSet) func() error {
			return resources.NewEditOptsForTest(flags).Validate
		},
	}

	mixedCases := []struct {
		name string
		set  map[string]string
	}{
		{name: "-o yaml with --json", set: map[string]string{"output": "yaml", "json": "metadata.name"}},
		{name: "-o yaml with --jq", set: map[string]string{"output": "yaml", "jq": ".metadata"}},
		{name: "unknown -o with --json", set: map[string]string{"output": "bogus", "json": "metadata.name"}},
	}

	for cmdName, newValidate := range validators {
		for _, tc := range mixedCases {
			t.Run(cmdName+" "+tc.name, func(t *testing.T) {
				flags := pflag.NewFlagSet(cmdName, pflag.ContinueOnError)
				validate := newValidate(flags)
				for flag, value := range tc.set {
					if err := flags.Set(flag, value); err != nil {
						t.Fatalf("set --%s: %v", flag, err)
					}
				}

				err := validate()
				if err == nil {
					t.Fatal("Validate() = nil, want typed rejection error")
				}
				assertUsageErrorExitCode(t, err)
			})
		}
	}
}

// TestPullAndEditDoNotAdvertiseAgentsFormat: both commands reject -o agents
// at validation time, so their -o usage menu must not advertise it.
func TestPullAndEditDoNotAdvertiseAgentsFormat(t *testing.T) {
	agent.SetFlag(false)
	t.Cleanup(func() { agent.SetFlag(false) })

	pullFlags := pflag.NewFlagSet("pull", pflag.ContinueOnError)
	resources.NewPullOptsForTest(pullFlags)
	editFlags := pflag.NewFlagSet("edit", pflag.ContinueOnError)
	resources.NewEditOptsForTest(editFlags)

	for name, flags := range map[string]*pflag.FlagSet{"pull": pullFlags, "edit": editFlags} {
		usage := flags.Lookup("output").Usage
		if strings.Contains(usage, "agents") {
			t.Errorf("%s -o usage still advertises the rejected agents format: %q", name, usage)
		}
		for _, want := range []string{"json", "yaml"} {
			if !strings.Contains(usage, want) {
				t.Errorf("%s -o usage missing %q: %q", name, want, usage)
			}
		}

		// --json/--jq are rejected on every use (TestPullAndEditRejectJSONAndJQ),
		// so help must not advertise them either.
		for _, rejected := range []string{"json", "jq"} {
			f := flags.Lookup(rejected)
			if f == nil {
				t.Fatalf("%s --%s flag not bound", name, rejected)
			}
			if !f.Hidden {
				t.Errorf("%s --%s is always rejected but still advertised in help", name, rejected)
			}
		}
	}
}
