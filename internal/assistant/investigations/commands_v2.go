package investigations

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"

	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// requireV2 probes the connected stack and returns a friendly error when
// Lodestone is not available.
func requireV2(cmd *cobra.Command, loader *providers.ConfigLoader) (*Client, error) {
	cfg, err := loader.LoadGrafanaConfig(cmd.Context())
	if err != nil {
		return nil, err
	}
	base, err := assistanthttp.NewClient(cfg)
	if err != nil {
		return nil, err
	}
	c, err := DetectCapability(cmd.Context(), base, cfg.Host)
	if err != nil {
		return nil, err
	}
	if !c.V2 {
		return nil, fmt.Errorf("%w on %s; use `gcx assistant investigations list` to see legacy investigations",
			errV2NotSupported, cfg.Host)
	}
	return NewClient(base), nil
}

// resolveChatID maps an investigation ID (user-facing) to a chat ID (v2 key).
func resolveChatID(ctx context.Context, client *Client, investigationID string) (string, error) {
	chatID, status, err := client.ResolveByID(ctx, investigationID)
	if err != nil {
		return "", err
	}
	if status == http.StatusNotFound {
		return "", fmt.Errorf("lodestone investigation %q not found", investigationID)
	}
	return chatID, nil
}

// --- pause ---

type pauseOpts struct{ IO cmdio.Options }

func (o *pauseOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
}

func newPauseCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &pauseOpts{}
	cmd := &cobra.Command{
		Use:   "pause <id>",
		Short: "Pause a running Lodestone investigation.",
		Long:  "Pause a Lodestone (v2) investigation. Unlike cancel, paused investigations can be resumed.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := requireV2(cmd, loader)
			if err != nil {
				return err
			}
			chatID, err := resolveChatID(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			msg, err := client.Pause(cmd.Context(), chatID)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), msg)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- resume ---

type resumeOpts struct{ IO cmdio.Options }

func (o *resumeOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
}

//nolint:dupl // sibling v2 commands share the same boilerplate by design
func newResumeCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &resumeOpts{}
	cmd := &cobra.Command{
		Use:   "resume <id>",
		Short: "Resume a paused Lodestone investigation.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := requireV2(cmd, loader)
			if err != nil {
				return err
			}
			chatID, err := resolveChatID(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			msg, err := client.Resume(cmd.Context(), chatID)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), msg)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- mode ---

type modeOpts struct{ IO cmdio.Options }

func (o *modeOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
}

func newModeCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &modeOpts{}
	cmd := &cobra.Command{
		Use:   "mode <id> <mode>",
		Short: "Change autonomy mode of a Lodestone investigation.",
		Long:  "Change the autonomy mode of a running Lodestone investigation. Valid modes: low, medium, high.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			validModes := []string{"low", "medium", "high"}
			mode := strings.ToLower(args[1])
			if !slices.Contains(validModes, mode) {
				return fmt.Errorf("invalid mode %q: must be one of %s", args[1], strings.Join(validModes, ", "))
			}
			client, err := requireV2(cmd, loader)
			if err != nil {
				return err
			}
			chatID, err := resolveChatID(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			resp, err := client.SetMode(cmd.Context(), chatID, mode)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- share ---

type shareOpts struct {
	IO    cmdio.Options
	Teams []string
}

func (o *shareOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
	flags.StringSliceVar(&o.Teams, "team", nil, "Team name to share with (repeatable)")
}

func (o *shareOpts) Validate() error {
	if len(o.Teams) == 0 {
		return errors.New("--team is required (one or more team names to share with)")
	}
	return nil
}

func newShareCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &shareOpts{}
	cmd := &cobra.Command{
		Use:   "share <id>",
		Short: "Share a Lodestone investigation with additional teams.",
		Long:  "Widen the visibility of a Lodestone investigation. Sharing is additive — teams cannot be removed.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := requireV2(cmd, loader)
			if err != nil {
				return err
			}
			resp, err := client.Scope(cmd.Context(), args[0], opts.Teams)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- regenerate-report ---

type regenReportOpts struct{ IO cmdio.Options }

func (o *regenReportOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
}

//nolint:dupl // sibling v2 commands share the same boilerplate by design
func newRegenerateReportCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &regenReportOpts{}
	cmd := &cobra.Command{
		Use:   "regenerate-report <id>",
		Short: "Queue regeneration of a Lodestone investigation report.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := requireV2(cmd, loader)
			if err != nil {
				return err
			}
			chatID, err := resolveChatID(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			msg, err := client.RegenerateReport(cmd.Context(), chatID)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), msg)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- repair-mermaid ---

type repairMermaidOpts struct {
	IO      cmdio.Options
	Message string
}

func (o *repairMermaidOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Message, "message", "", "Original Mermaid parser error message to guide the repair")
}

func newRepairMermaidCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &repairMermaidOpts{}
	cmd := &cobra.Command{
		Use:   "repair-mermaid <id> <element-id>",
		Short: "Ask the server to LLM-repair a broken Mermaid diagram.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := requireV2(cmd, loader)
			if err != nil {
				return err
			}
			chatID, err := resolveChatID(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			resp, err := client.RepairMermaid(cmd.Context(), chatID, args[1], opts.Message)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- update-mermaid ---

type updateMermaidOpts struct {
	IO      cmdio.Options
	Content string
}

func (o *updateMermaidOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Content, "content", "", `Mermaid source to persist. Path to a file, or "-" to read from stdin.`)
}

func (o *updateMermaidOpts) Validate() error {
	if o.Content == "" {
		return errors.New("--content is required (path to a file or \"-\" for stdin)")
	}
	return nil
}

func (o *updateMermaidOpts) readContent(stdin io.Reader) (string, error) {
	if o.Content == "-" {
		data, err := io.ReadAll(stdin)
		if err != nil {
			return "", fmt.Errorf("read content from stdin: %w", err)
		}
		return string(data), nil
	}
	data, err := os.ReadFile(o.Content)
	if err != nil {
		return "", fmt.Errorf("read content from %s: %w", o.Content, err)
	}
	return string(data), nil
}

func newUpdateMermaidCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &updateMermaidOpts{}
	cmd := &cobra.Command{
		Use:   "update-mermaid <id> <element-id>",
		Short: "Persist Mermaid source for a Lodestone report element.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			content, err := opts.readContent(cmd.InOrStdin())
			if err != nil {
				return err
			}
			client, err := requireV2(cmd, loader)
			if err != nil {
				return err
			}
			chatID, err := resolveChatID(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			resp, err := client.UpdateMermaid(cmd.Context(), chatID, args[1], content)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- state ---
//
// Distinct from `get`: state always uses the v2 endpoint and returns the full
// session state (plan, hypotheses, report, mode, epoch). `get` may fall back
// to v1 for legacy investigations.

type stateOpts struct{ IO cmdio.Options }

func (o *stateOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
}

//nolint:dupl // sibling v2 commands share the same boilerplate by design
func newStateCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &stateOpts{}
	cmd := &cobra.Command{
		Use:   "state <id>",
		Short: "Show full Lodestone session state (plan, hypotheses, mode, report).",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := requireV2(cmd, loader)
			if err != nil {
				return err
			}
			chatID, err := resolveChatID(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			state, err := client.GetState(cmd.Context(), chatID)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), state)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}
