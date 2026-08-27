package rules

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/goccy/go-yaml"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/agento11y/eval"
	"github.com/spf13/cobra"
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

func readActionFile(path string, in io.Reader) (*eval.RuleAction, error) {
	data, err := ReadFile(path, in)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var f actionFile
	if err := json.Unmarshal(data, &f); err != nil {
		if yerr := yaml.Unmarshal(data, &f); yerr != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, yerr)
		}
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

func encodeAction(w io.Writer, value any) error { return json.NewEncoder(w).Encode(value) }

func actionListCommand(loader *providers.ConfigLoader) *cobra.Command {
	return &cobra.Command{Use: "list <rule-id>", Short: "List actions attached to an evaluation rule.", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		c, err := actionClient(cmd, loader)
		if err != nil {
			return err
		}
		items, err := c.ListActions(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		return encodeAction(cmd.OutOrStdout(), items)
	}}
}

func actionGetCommand(loader *providers.ConfigLoader) *cobra.Command {
	return &cobra.Command{Use: "get <rule-id> <action-id>", Short: "Get an action attached to an evaluation rule.", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		c, err := actionClient(cmd, loader)
		if err != nil {
			return err
		}
		item, err := c.GetAction(cmd.Context(), args[0], args[1])
		if err != nil {
			return err
		}
		return encodeAction(cmd.OutOrStdout(), item)
	}}
}

func actionCreateCommand(loader *providers.ConfigLoader) *cobra.Command {
	var file string
	cmd := &cobra.Command{Use: "create <rule-id>", Short: "Create an action from a file.", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
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
		return encodeAction(cmd.OutOrStdout(), created)
	}}
	cmd.Flags().StringVarP(&file, "filename", "f", "", "File containing the action definition (use - for stdin)")
	return cmd
}

func actionUpdateCommand(loader *providers.ConfigLoader) *cobra.Command {
	var file string
	cmd := &cobra.Command{Use: "update <rule-id> <action-id>", Short: "Update an action from a file.", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if file == "" {
			return errors.New("--filename/-f is required")
		}
		action, err := readActionFile(file, cmd.InOrStdin())
		if err != nil {
			return err
		}
		action.ActionID = args[1]
		c, err := actionClient(cmd, loader)
		if err != nil {
			return err
		}
		updated, err := c.UpdateAction(cmd.Context(), args[0], action)
		if err != nil {
			return err
		}
		return encodeAction(cmd.OutOrStdout(), updated)
	}}
	cmd.Flags().StringVarP(&file, "filename", "f", "", "File containing the action definition (use - for stdin)")
	return cmd
}

func actionDeleteCommand(loader *providers.ConfigLoader) *cobra.Command {
	var force bool
	cmd := &cobra.Command{Use: "delete <rule-id> <action-id>", Short: "Delete an action attached to an evaluation rule.", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		proceed, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.ErrOrStderr(), force, fmt.Sprintf("Delete action %s?", args[1]))
		if err != nil || !proceed {
			return err
		}
		c, err := actionClient(cmd, loader)
		if err != nil {
			return err
		}
		return c.DeleteAction(cmd.Context(), args[0], args[1])
	}}
	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompt")
	return cmd
}

func reconcileActions(ctx context.Context, c *Client, ruleID string, desired *[]eval.RuleAction) ([]eval.RuleAction, error) {
	if desired == nil {
		return c.ListActions(ctx, ruleID)
	}
	existing, err := c.ListActions(ctx, ruleID)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]eval.RuleAction, len(existing))
	for _, a := range existing {
		byID[a.ActionID] = a
	}
	result := make([]eval.RuleAction, 0, len(*desired))
	seen := make(map[string]bool)
	for _, a := range *desired {
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
	for id := range byID {
		if !seen[id] {
			if err := c.DeleteAction(ctx, ruleID, id); err != nil {
				return nil, err
			}
		}
	}
	return result, nil
}
