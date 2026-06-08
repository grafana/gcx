package investigations

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// requireV2 probes the connected stack and returns a friendly error when the
// /api/v2 investigations surface is not available.
func requireV2(cmd *cobra.Command, loader *providers.ConfigLoader) (*Client, error) {
	cfg, err := loader.LoadGrafanaConfig(cmd.Context())
	if err != nil {
		return nil, err
	}
	base, err := assistanthttp.NewClient(cfg)
	if err != nil {
		return nil, err
	}
	mode, err := DetectAPIMode(cmd.Context(), loader, base)
	if err != nil {
		return nil, err
	}
	if !mode.SupportsV2() {
		return nil, fmt.Errorf("%w on %s; use `gcx assistant investigations list` to see legacy investigations",
			errV2NotSupported, cfg.Host)
	}
	return NewClient(base), nil
}

// resolveID maps a user-supplied investigation identifier to the canonical
// investigationId expected by /api/v2 endpoints.
func resolveID(ctx context.Context, client *Client, id string) (string, error) {
	resp, status, err := client.ResolveByID(ctx, id)
	if err != nil {
		return "", err
	}
	if status == http.StatusNotFound {
		return "", fmt.Errorf("investigation %q not found", id)
	}
	return resp.InvestigationID, nil
}

// resolveChatID maps a user-supplied investigation identifier to the chatId
// regardless of API mode. Used by chat-thread commands, which read from the
// v1 /chats/{chatId}/all-messages endpoint that's outside the investigations
// surface and not affected by the v2 rollout.
func resolveChatID(ctx context.Context, client *Client, id string) (string, error) {
	resp, status, err := client.ResolveByID(ctx, id)
	if err != nil {
		return "", err
	}
	if status == http.StatusNotFound {
		return "", fmt.Errorf("investigation %q not found", id)
	}
	return resp.ChatID, nil
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
		Short: "Pause a running v2 investigation.",
		Long:  "Pause a v2 investigation. Unlike cancel, paused investigations can be resumed.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := requireV2(cmd, loader)
			if err != nil {
				return err
			}
			chatID, err := resolveID(cmd.Context(), client, args[0])
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
		Short: "Resume a paused v2 investigation.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := requireV2(cmd, loader)
			if err != nil {
				return err
			}
			chatID, err := resolveID(cmd.Context(), client, args[0])
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

// Validate conforms modeOpts to the Options pattern. Mode is a positional arg,
// so its enum check lives in validateMode and runs against args[1].
func (o *modeOpts) Validate() error { return nil }

func validateMode(raw string) (string, error) {
	validModes := []string{"low", "medium", "high"}
	mode := strings.ToLower(raw)
	if !slices.Contains(validModes, mode) {
		return "", fmt.Errorf("invalid mode %q: must be one of %s", raw, strings.Join(validModes, ", "))
	}
	return mode, nil
}

func newModeCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &modeOpts{}
	cmd := &cobra.Command{
		Use:   "mode <id> <mode>",
		Short: "Change autonomy mode of a v2 investigation.",
		Long:  "Change the autonomy mode of a running v2 investigation. Valid modes: low, medium, high.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			if err := opts.Validate(); err != nil {
				return err
			}
			mode, err := validateMode(args[1])
			if err != nil {
				return err
			}
			client, err := requireV2(cmd, loader)
			if err != nil {
				return err
			}
			chatID, err := resolveID(cmd.Context(), client, args[0])
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
		Short: "Share a v2 investigation with additional teams.",
		Long:  "Widen the visibility of a v2 investigation. Sharing is additive — teams cannot be removed.",
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
			id, err := resolveID(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			resp, err := client.Scope(cmd.Context(), id, opts.Teams)
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
		Short: "Queue regeneration of a v2 investigation report.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := requireV2(cmd, loader)
			if err != nil {
				return err
			}
			chatID, err := resolveID(cmd.Context(), client, args[0])
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
