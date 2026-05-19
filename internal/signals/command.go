package signals

import (
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
)

// CommandBuilder constructs a Cobra command using the signal's shared config loader.
type CommandBuilder func(*providers.ConfigLoader) *cobra.Command

// CommandSpec describes a command mounted under a top-level signal area such as
// metrics, logs, traces, or profiles.
type CommandSpec struct {
	Build     CommandBuilder
	TokenCost string
	LLMHint   string
	Example   string
}

// AdaptiveSpec describes an adaptive telemetry subtree mounted under a signal.
type AdaptiveSpec struct {
	Build CommandBuilder
	Use   string
	Short string
}

// Descriptor captures the common structure of top-level signal commands.
type Descriptor struct {
	Name          string
	Short         string
	Commands      []CommandSpec
	ExtraCommands []CommandBuilder
	Adaptive      *AdaptiveSpec
	ConfigKeys    []providers.ConfigKey
	Registrations func(*providers.ConfigLoader) []adapter.Registration
}

// Command builds the top-level signal Cobra command from a descriptor.
func Command(desc Descriptor) *cobra.Command {
	loader := &providers.ConfigLoader{}

	cmd := &cobra.Command{
		Use:   desc.Name,
		Short: desc.Short,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if root := cmd.Root(); root.PersistentPreRun != nil {
				root.PersistentPreRun(cmd, args)
			}
		},
	}

	loader.BindFlags(cmd.PersistentFlags())

	for _, spec := range desc.Commands {
		if spec.Build == nil {
			continue
		}
		sub := spec.Build(loader)
		ApplyCommandMetadata(sub, spec)
		cmd.AddCommand(sub)
	}

	for _, build := range desc.ExtraCommands {
		if build == nil {
			continue
		}
		cmd.AddCommand(build(loader))
	}

	if desc.Adaptive != nil && desc.Adaptive.Build != nil {
		adaptiveCmd := desc.Adaptive.Build(loader)
		if desc.Adaptive.Use != "" {
			adaptiveCmd.Use = desc.Adaptive.Use
		}
		if desc.Adaptive.Short != "" {
			adaptiveCmd.Short = desc.Adaptive.Short
		}
		cmd.AddCommand(adaptiveCmd)
	}

	return cmd
}

// ApplyCommandMetadata applies signal-specific examples and agent annotations to
// a command built by a datasource package.
func ApplyCommandMetadata(cmd *cobra.Command, spec CommandSpec) {
	if spec.Example != "" {
		cmd.Example = spec.Example
	}
	if spec.TokenCost == "" && spec.LLMHint == "" {
		return
	}
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	if spec.TokenCost != "" {
		cmd.Annotations[agent.AnnotationTokenCost] = spec.TokenCost
	}
	if spec.LLMHint != "" {
		cmd.Annotations[agent.AnnotationLLMHint] = spec.LLMHint
	}
}

// TypedRegistrations returns adapter registrations declared by the descriptor.
func (desc Descriptor) TypedRegistrations() []adapter.Registration {
	if desc.Registrations == nil {
		return nil
	}
	return desc.Registrations(&providers.ConfigLoader{})
}
