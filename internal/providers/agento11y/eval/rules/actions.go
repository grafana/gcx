package rules

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/goccy/go-yaml"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/agento11y/commandutil"
	"github.com/grafana/gcx/internal/providers/agento11y/eval"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/sync/errgroup"
)

func actionsCommand(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{Use: "actions", Short: "Manage actions attached to an evaluation rule."}
	cmd.AddCommand(actionListCommand(loader), actionGetCommand(loader), actionCreateCommand(loader), actionUpdateCommand(loader), actionDeleteCommand(loader))
	return cmd
}

type actionFile struct {
	Condition    eval.RuleActionCondition `json:"condition" yaml:"condition"`
	ActionConfig eval.RuleActionConfig    `json:"action_config" yaml:"action_config"`
	Enabled      *bool                    `json:"enabled,omitempty" yaml:"enabled,omitempty"`
}

// parseActionFile decodes an action definition. Enabled stays nil when the
// file omits it, so callers decide what an omitted value means.
func parseActionFile(path string, in io.Reader) (*actionFile, error) {
	data, err := ReadFile(path, in)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var f actionFile
	if jerr := json.Unmarshal(data, &f); jerr != nil {
		f = actionFile{}
		if yerr := yaml.Unmarshal(data, &f); yerr != nil {
			return nil, fmt.Errorf("parsing %s: not valid JSON (%w) or YAML (%w)", path, jerr, yerr)
		}
	}
	return &f, nil
}

// readActionFile reads an action for creation, where an omitted enabled means
// enabled.
func readActionFile(path string, in io.Reader) (*eval.RuleAction, error) {
	f, err := parseActionFile(path, in)
	if err != nil {
		return nil, err
	}
	enabled := true
	if f.Enabled != nil {
		enabled = *f.Enabled
	}
	return &eval.RuleAction{Condition: f.Condition, ActionConfig: f.ActionConfig, Enabled: enabled}, nil
}

func actionClient(cmd *cobra.Command, loader *providers.ConfigLoader) (*Client, error) {
	return NewClientForLoader(cmd.Context(), loader)
}

type actionOutputOpts struct{ IO cmdio.Options }

func (o *actionOutputOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(flags)
}

func actionListCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &actionOutputOpts{}
	cmd := &cobra.Command{Use: "list <rule-id>", Short: "List actions attached to an evaluation rule.", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := opts.IO.Validate(); err != nil {
			return err
		}
		c, err := actionClient(cmd, loader)
		if err != nil {
			return err
		}
		items, err := c.ListActions(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		return opts.IO.Encode(cmd.OutOrStdout(), items)
	}}
	opts.setup(cmd.Flags())
	return cmd
}

func actionGetCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &actionOutputOpts{}
	cmd := &cobra.Command{Use: "get <rule-id> <action-id>", Short: "Get an action attached to an evaluation rule.", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if err := opts.IO.Validate(); err != nil {
			return err
		}
		c, err := actionClient(cmd, loader)
		if err != nil {
			return err
		}
		item, err := c.GetAction(cmd.Context(), args[0], args[1])
		if err != nil {
			return err
		}
		return opts.IO.Encode(cmd.OutOrStdout(), item)
	}}
	opts.setup(cmd.Flags())
	return cmd
}

func actionCreateCommand(loader *providers.ConfigLoader) *cobra.Command {
	var file string
	opts := &actionOutputOpts{}
	cmd := &cobra.Command{Use: "create <rule-id>", Short: "Create an action from a file.", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := opts.IO.Validate(); err != nil {
			return err
		}
		if file == "" {
			return errors.New("--filename/-f is required")
		}
		action, err := readActionFile(file, cmd.InOrStdin())
		if err != nil {
			return err
		}
		c, err := actionClient(cmd, loader)
		if err != nil {
			return err
		}
		created, err := c.CreateAction(cmd.Context(), args[0], action)
		if err != nil {
			return err
		}
		cmdio.Success(cmd.ErrOrStderr(), "Action %s created", created.ActionID)
		return opts.IO.Encode(cmd.OutOrStdout(), created)
	}}
	cmd.Flags().StringVarP(&file, "filename", "f", "", "File containing the action definition (use - for stdin)")
	opts.setup(cmd.Flags())
	return cmd
}

func actionUpdateCommand(loader *providers.ConfigLoader) *cobra.Command {
	var file string
	opts := &actionOutputOpts{}
	cmd := &cobra.Command{Use: "update <rule-id> <action-id>", Short: "Update an action from a file.", Long: "Update an action's condition and config from a file. When the file omits enabled, the action's stored enabled state is left unchanged.", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if err := opts.IO.Validate(); err != nil {
			return err
		}
		if file == "" {
			return errors.New("--filename/-f is required")
		}
		f, err := parseActionFile(file, cmd.InOrStdin())
		if err != nil {
			return err
		}
		action := &eval.RuleAction{ActionID: args[1], Condition: f.Condition, ActionConfig: f.ActionConfig}
		c, err := actionClient(cmd, loader)
		if err != nil {
			return err
		}
		// An omitted enabled leaves the stored state alone instead of re-enabling.
		updated, err := c.PatchAction(cmd.Context(), args[0], action, f.Enabled)
		if err != nil {
			return err
		}
		cmdio.Success(cmd.ErrOrStderr(), "Action %s updated", updated.ActionID)
		return opts.IO.Encode(cmd.OutOrStdout(), updated)
	}}
	cmd.Flags().StringVarP(&file, "filename", "f", "", "File containing the action definition (use - for stdin)")
	opts.setup(cmd.Flags())
	return cmd
}

func actionDeleteCommand(loader *providers.ConfigLoader) *cobra.Command {
	var force bool
	opts := &deleteOpts{}
	cmd := &cobra.Command{Use: "delete <rule-id> <action-id>", Short: "Delete an action attached to an evaluation rule.", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if err := opts.IO.Validate(); err != nil {
			return err
		}
		proceed, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.ErrOrStderr(), force, fmt.Sprintf("Delete action %s?", args[1]))
		if err != nil || !proceed {
			return err
		}
		c, err := actionClient(cmd, loader)
		if err != nil {
			return err
		}
		return commandutil.RunBatchDelete(cmd.OutOrStdout(), cmd.ErrOrStderr(), &opts.IO, "rule action", "Deleted action %s", "deleting action %s", []string{args[1]}, func(id string) error { return c.DeleteAction(cmd.Context(), args[0], id) })
	}}
	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompt")
	opts.IO.RegisterCustomCodec("text", commandutil.SilentTextCodec{})
	opts.IO.DefaultFormat("text")
	opts.IO.BindFlags(cmd.Flags())
	return cmd
}

// reconcileOpts controls how a desired action list is applied to a rule.
type reconcileOpts struct {
	// Prune deletes existing actions that are absent from the desired list.
	// Without it, unlisted actions are left alone.
	Prune bool
	// MatchByCondition reuses the existing action with the same condition and
	// action kind for each desired action without an ID, so repeated runs of
	// the --on-*-collection flags update an action instead of duplicating it.
	MatchByCondition bool
}

func reconcileActions(ctx context.Context, c *Client, ruleID string, desired []eval.RuleAction, opts reconcileOpts) ([]eval.RuleAction, error) {
	existing, err := c.ListActions(ctx, ruleID)
	if err != nil {
		return nil, err
	}
	if opts.MatchByCondition {
		used := make(map[string]bool)
		for i := range desired {
			if desired[i].ActionID != "" {
				continue
			}
			for _, e := range existing {
				if !used[e.ActionID] && e.Condition.Kind == desired[i].Condition.Kind && e.ActionConfig.Kind == desired[i].ActionConfig.Kind {
					desired[i].ActionID = e.ActionID
					used[e.ActionID] = true
					break
				}
			}
		}
	}
	result := make([]eval.RuleAction, 0, len(desired))
	seen := make(map[string]bool)
	for _, a := range desired {
		var got *eval.RuleAction
		if a.ActionID != "" {
			got, err = c.UpdateAction(ctx, ruleID, &a)
			seen[a.ActionID] = true
		} else {
			got, err = c.CreateAction(ctx, ruleID, &a)
		}
		if err != nil {
			return nil, err
		}
		result = append(result, *got)
	}
	if !opts.Prune {
		return result, nil
	}
	for _, e := range existing {
		if !seen[e.ActionID] {
			if err := c.DeleteAction(ctx, ruleID, e.ActionID); err != nil {
				return nil, err
			}
		}
	}
	return result, nil
}

// attachActions loads each rule's actions for read commands. A rule whose
// actions cannot be read is returned without them and counted in the warning,
// so a backend without the actions endpoint doesn't break rules list.
func attachActions(ctx context.Context, c *Client, specs []eval.RuleDefinition, stderr io.Writer) error {
	errs := make([]error, len(specs))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(10)
	for i := range specs {
		g.Go(func() error {
			actions, err := c.ListActions(gctx, specs[i].RuleID)
			if err != nil {
				errs[i] = err
				return nil //nolint:nilerr // Failures are collected per rule and reported as one warning.
			}
			specs[i].Actions = &actions
			return nil
		})
	}
	_ = g.Wait()
	if err := ctx.Err(); err != nil {
		return err
	}
	failed := 0
	var first error
	var firstID string
	for i, err := range errs {
		if err == nil {
			continue
		}
		if failed == 0 {
			first, firstID = err, specs[i].RuleID
		}
		failed++
	}
	if failed > 0 {
		warnActionsUnavailable(stderr, failed, firstID, first)
	}
	return nil
}

func warnActionsUnavailable(stderr io.Writer, failed int, ruleID string, err error) {
	cmdio.EmitWarn(stderr, fmt.Sprintf("could not read actions for %d rule(s), omitting them (first: rule %s: %v)", failed, ruleID, err))
}
