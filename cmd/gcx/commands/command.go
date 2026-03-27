package commands

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// CommandInfo describes a CLI command with rich metadata for agent consumption.
type CommandInfo struct {
	FullPath       string        `json:"full_path"`
	Description    string        `json:"description"`
	Long           string        `json:"long,omitempty"`
	Example        string        `json:"example,omitempty"`
	TokenCost      string        `json:"token_cost,omitempty"`
	LLMHint        string        `json:"llm_hint,omitempty"`
	RequiredScope  string        `json:"required_scope,omitempty"`
	RequiredRole   string        `json:"required_role,omitempty"`
	RequiredAction string        `json:"required_action,omitempty"`
	Args           string        `json:"args,omitempty"`
	Flags          []FlagInfo    `json:"flags,omitempty"`
	Subcommands    []CommandInfo `json:"subcommands,omitempty"`
}

// FlagInfo describes a single CLI flag.
type FlagInfo struct {
	Name        string `json:"name"`
	Shorthand   string `json:"shorthand,omitempty"`
	Type        string `json:"type"`
	Default     string `json:"default"`
	Description string `json:"description"`
}

// ResourceOperationInfo describes agent metadata for a single resource operation.
type ResourceOperationInfo struct {
	TokenCost string `json:"token_cost"` // "small", "medium", "large"
	LLMHint   string `json:"llm_hint"`   // example command
}

// ResourceTypeInfo describes a Grafana resource type with agent metadata.
type ResourceTypeInfo struct {
	Kind       string                           `json:"kind"`
	Group      string                           `json:"group"`
	Version    string                           `json:"version"`
	Aliases    []string                         `json:"aliases,omitempty"`
	Operations map[string]ResourceOperationInfo `json:"operations,omitempty"` // keyed by "get", "push", "pull", "delete"
	Source     string                           `json:"source"`               // "well-known", "adapter"
}

// CatalogOutput is the top-level output for the hierarchical (default) mode.
type CatalogOutput struct {
	Commands      CommandInfo        `json:"commands"`
	ResourceTypes []ResourceTypeInfo `json:"resource_types"`
}

// FlatCatalogOutput is the top-level output for --flat mode.
type FlatCatalogOutput struct {
	Commands      []CommandInfo      `json:"commands"`
	ResourceTypes []ResourceTypeInfo `json:"resource_types"`
}

type commandsOpts struct {
	IO            cmdio.Options
	Flat          bool
	IncludeHidden bool
	ValidateLive  bool
}

func (opts *commandsOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("text", &commandsTextCodec{})
	opts.IO.DefaultFormat("json")
	opts.IO.BindFlags(flags)
	flags.BoolVar(&opts.Flat, "flat", false, "Flatten the command tree into a single list")
	flags.BoolVar(&opts.IncludeHidden, "include-hidden", false, "Include hidden commands in the output")
	flags.BoolVar(&opts.ValidateLive, "validate", false, "Validate catalog against a live Grafana instance (requires configured context)")
}

func (opts *commandsOpts) Validate() error {
	return opts.IO.Validate()
}

// Command returns the "commands" command that outputs a hierarchical catalog.
// configOpts is optional — only needed for --validate (pass nil to disable).
func Command(root *cobra.Command, configOpts ...*cmdconfig.Options) *cobra.Command {
	opts := &commandsOpts{}

	// Use the first configOpts if provided.
	var cfgOpts *cmdconfig.Options
	if len(configOpts) > 0 {
		cfgOpts = configOpts[0]
	}

	cmd := &cobra.Command{
		Use:   "commands",
		Short: "List all commands with rich metadata for agent consumption",
		Long: `Output a hierarchical catalog of all CLI commands with metadata including
flags, arguments, token cost estimates, and agent hints. Also includes a
resource_types section listing all known Grafana resource types.

Use --validate with a configured Grafana context to compare the catalog
against live resource discovery and report uncovered or stale types.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			tree := walkCommandWithOptions(root, "", opts.IncludeHidden)
			resourceTypes := collectResourceTypes(agent.KnownResources, adapter.AllRegistrations())

			// --validate: compare catalog against live Grafana instance.
			if opts.ValidateLive {
				if cfgOpts == nil {
					return errors.New("--validate requires a configured Grafana context")
				}
				ctx := cmd.Context()
				cfg, err := cfgOpts.LoadGrafanaConfig(ctx)
				if err != nil {
					return fmt.Errorf("--validate requires a valid Grafana connection: %w", err)
				}
				result, err := validateAgainstLive(ctx, cfg, resourceTypes)
				if err != nil {
					return err
				}
				writeValidationReport(cmd.OutOrStdout(), result)
				if len(result.Uncovered) > 0 {
					return fmt.Errorf("%d resource types not covered by catalog", len(result.Uncovered))
				}
				return nil
			}

			if opts.Flat {
				flat := flattenCommands(tree)
				output := FlatCatalogOutput{
					Commands:      flat,
					ResourceTypes: resourceTypes,
				}
				return opts.IO.Encode(cmd.OutOrStdout(), output)
			}

			output := CatalogOutput{
				Commands:      tree,
				ResourceTypes: resourceTypes,
			}
			return opts.IO.Encode(cmd.OutOrStdout(), output)
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}

// walkCommand walks the command tree recursively, excluding hidden commands.
func walkCommand(cmd *cobra.Command, parentPath string) CommandInfo {
	return walkCommandWithOptions(cmd, parentPath, false)
}

// walkCommandWithOptions walks the command tree with configurable hidden command inclusion.
func walkCommandWithOptions(cmd *cobra.Command, parentPath string, includeHidden bool) CommandInfo {
	fullPath := cmd.Name()
	if parentPath != "" {
		fullPath = parentPath + " " + cmd.Name()
	}

	info := CommandInfo{
		FullPath:       fullPath,
		Description:    cmd.Short,
		Long:           cmd.Long,
		Example:        cmd.Example,
		TokenCost:      cmd.Annotations[agent.AnnotationTokenCost],
		LLMHint:        cmd.Annotations[agent.AnnotationLLMHint],
		RequiredScope:  cmd.Annotations[agent.AnnotationRequiredScope],
		RequiredRole:   cmd.Annotations[agent.AnnotationRequiredRole],
		RequiredAction: cmd.Annotations[agent.AnnotationRequiredAction],
		Args:           extractArgs(cmd.Use),
	}

	// Collect non-inherited flags.
	cmd.NonInheritedFlags().VisitAll(func(f *pflag.Flag) {
		info.Flags = append(info.Flags, FlagInfo{
			Name:        f.Name,
			Shorthand:   f.Shorthand,
			Type:        f.Value.Type(),
			Default:     f.DefValue,
			Description: f.Usage,
		})
	})

	// Recurse into subcommands.
	for _, sub := range cmd.Commands() {
		if sub.Hidden && !includeHidden {
			continue
		}
		info.Subcommands = append(info.Subcommands, walkCommandWithOptions(sub, fullPath, includeHidden))
	}

	return info
}

// extractArgs extracts the argument pattern from a cobra Use string.
// Use format is "command [ARGS]..." — everything after the first space is the args pattern.
func extractArgs(use string) string {
	_, args, found := strings.Cut(use, " ")
	if !found {
		return ""
	}
	return args
}

// flattenCommands converts a tree of CommandInfo into a flat slice, depth-first.
func flattenCommands(root CommandInfo) []CommandInfo {
	children := root.Subcommands
	root.Subcommands = nil
	result := make([]CommandInfo, 0, 1+len(children))
	result = append(result, root)
	for _, child := range children {
		result = append(result, flattenCommands(child)...)
	}
	return result
}

// collectResourceTypes merges well-known K8s types and adapter registrations.
func collectResourceTypes(wellKnown []agent.KnownResource, regs []adapter.Registration) []ResourceTypeInfo {
	types := make([]ResourceTypeInfo, 0, len(wellKnown)+len(regs))

	for _, wk := range wellKnown {
		types = append(types, ResourceTypeInfo{
			Kind:       wk.Kind,
			Group:      wk.Group,
			Version:    wk.Version,
			Aliases:    wk.Aliases,
			Operations: convertOperations(wk.Operations),
			Source:     "well-known",
		})
	}

	for _, reg := range regs {
		types = append(types, ResourceTypeInfo{
			Kind:       reg.GVK.Kind,
			Group:      reg.GVK.Group,
			Version:    reg.GVK.Version,
			Aliases:    reg.Aliases,
			Operations: convertOperations(reg.Operations),
			Source:     "adapter",
		})
	}

	return types
}

// convertOperations converts agent.OperationHint map to ResourceOperationInfo map.
func convertOperations(ops map[string]agent.OperationHint) map[string]ResourceOperationInfo {
	if len(ops) == 0 {
		return nil
	}
	result := make(map[string]ResourceOperationInfo, len(ops))
	for k, v := range ops {
		result[k] = ResourceOperationInfo{TokenCost: v.TokenCost, LLMHint: v.LLMHint}
	}
	return result
}

// commandsTextCodec renders a simple table of commands.
type commandsTextCodec struct{}

func (c *commandsTextCodec) Format() format.Format { return "text" }

func (c *commandsTextCodec) Encode(output io.Writer, value any) error {
	tab := tabwriter.NewWriter(output, 0, 4, 2, ' ', tabwriter.TabIndent|tabwriter.DiscardEmptyColumns)

	switch v := value.(type) {
	case CatalogOutput:
		fmt.Fprintf(tab, "COMMAND\tDESCRIPTION\tTOKEN_COST\n")
		writeCommandTable(tab, v.Commands, "")
	case FlatCatalogOutput:
		fmt.Fprintf(tab, "COMMAND\tDESCRIPTION\tTOKEN_COST\n")
		for _, cmd := range v.Commands {
			fmt.Fprintf(tab, "%s\t%s\t%s\n", cmd.FullPath, cmd.Description, cmd.TokenCost)
		}
	default:
		return fmt.Errorf("unsupported type for text codec: %T", value)
	}

	return tab.Flush()
}

func writeCommandTable(w io.Writer, info CommandInfo, indent string) {
	fmt.Fprintf(w, "%s%s\t%s\t%s\n", indent, info.FullPath, info.Description, info.TokenCost)
	for _, sub := range info.Subcommands {
		writeCommandTable(w, sub, indent)
	}
}

func (c *commandsTextCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("commands text codec does not support decoding")
}
