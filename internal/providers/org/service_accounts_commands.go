package org

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/shared"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type accountOpts struct {
	IO        cmdio.Options
	Role      string
	File      string
	TTL       string
	AccountID int64
	Force     bool
	DryRun    bool
	Limit     int
	Filter    string
}

func (o *accountOpts) setup(flags *pflag.FlagSet) {
	o.IO.BindFlags(flags)
}
func (o *accountOpts) Validate() error { return o.IO.Validate() }
func (o *accountOpts) mutationFlags(flags *pflag.FlagSet, destructive bool) {
	o.IO.RegisterCustomCodec("table", orgMutationTable().Codec("table"))
	o.IO.DefaultFormat("table")
	o.setup(flags)
	flags.BoolVar(&o.DryRun, "dry-run", false, "Show the local plan without a write request or a file write")
	if destructive {
		flags.BoolVar(&o.Force, "force", false, "Skip the confirmation prompt")
	}
}
func positiveID(value string) (int64, error) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid ID %q: use a positive integer, for example 123", value)
	}
	return id, nil
}
func accountTarget(id int64) cmdio.MutationTarget {
	return cmdio.MutationTarget{Kind: "ServiceAccount", ID: strconv.FormatInt(id, 10)}
}
func smallCommand(use, short string) *cobra.Command {
	return &cobra.Command{Use: use, Short: short, Annotations: map[string]string{agent.AnnotationTokenCost: "small"}}
}
func newServiceAccountsCommand(loader providers.GrafanaConfigLoader) *cobra.Command {
	cmd := &cobra.Command{Use: "service-accounts", Short: "Manage service accounts in the selected organization"}
	cmd.AddCommand(newAccountsListCommand(loader), newAccountCreateCommand(loader), newAccountDeleteCommand(loader), newTokenCreateCommand(loader), newTokenDeleteCommand(loader))
	return cmd
}
func newAccountsListCommand(loader providers.GrafanaConfigLoader) *cobra.Command {
	o := &accountOpts{}
	cmd := smallCommand("list", "List service accounts in the selected organization")
	cmd.Args = cobra.NoArgs
	cmd.Long = "Fetch all service account pages from the selected organization. Apply the output limit after the fetch."
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if err := o.Validate(); err != nil {
			return err
		}
		c, err := loadClient(cmd, loader)
		if err != nil {
			return err
		}
		accounts, err := c.serviceAccounts(cmd.Context())
		if err != nil {
			return err
		}
		accounts, meta := cmdio.TruncateCompleteList(accounts, o.Limit)
		result := accountListResult{accounts, cmdio.AttachListMeta(meta, os.Args)}
		if err := o.IO.Encode(cmd.OutOrStdout(), result); err != nil {
			return err
		}
		cmdio.EmitListTruncationHint(cmd.ErrOrStderr(), meta)
		return nil
	}
	o.IO.RegisterCustomCodec("table", accountListTable().Codec("table"))
	o.IO.DefaultFormat("table")
	o.setup(cmd.Flags())
	o.IO.BindListLimit(cmd.Flags(), &o.Limit, "service accounts", 50)
	return cmd
}
func newAccountCreateCommand(loader providers.GrafanaConfigLoader) *cobra.Command {
	o := &accountOpts{}
	cmd := smallCommand("create <name>", "Create a service account in the selected organization")
	cmd.Args = cobra.ExactArgs(1)
	cmd.Long = "Create a service account with a basic organization role. Use None with a later fixed RBAC role assignment for narrow access. A dry run validates inputs locally."
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := o.Validate(); err != nil {
			return err
		}
		if strings.TrimSpace(args[0]) == "" {
			return errors.New("service account name must not be empty")
		}
		switch o.Role {
		case "None", "Viewer", "Editor", "Admin":
		default:
			return fmt.Errorf("invalid role %q: use None, Viewer, Editor, or Admin", o.Role)
		}
		result := cmdio.NewSingleMutation("created", cmdio.MutationTarget{Kind: "ServiceAccount", Name: args[0]})
		result.DryRun = o.DryRun
		if o.DryRun {
			return o.IO.Encode(cmd.OutOrStdout(), result)
		}
		c, err := loadClient(cmd, loader)
		if err != nil {
			return err
		}
		var account ServiceAccount
		if err := c.send(cmd.Context(), http.MethodPost, "/api/serviceaccounts", map[string]any{"name": args[0], "role": o.Role, "isDisabled": false}, &account); err != nil {
			return err
		}
		result.Target = accountTarget(account.ID)
		result.Target.Name = account.Name
		return o.IO.Encode(cmd.OutOrStdout(), result)
	}
	o.mutationFlags(cmd.Flags(), false)
	cmd.Flags().StringVar(&o.Role, "role", "None", "Basic organization role: None, Viewer, Editor, or Admin (case-sensitive)")
	return cmd
}
func confirmOrgDelete(cmd *cobra.Command, force bool, prompt string) error {
	proceed, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.ErrOrStderr(), force, prompt)
	if err != nil {
		return err
	}
	if !proceed {
		code := gcxerrors.ExitCancelled
		return &gcxerrors.DetailedError{Summary: "organization deletion cancelled", ExitCode: &code}
	}
	return nil
}
func newAccountDeleteCommand(loader providers.GrafanaConfigLoader) *cobra.Command {
	o := &accountOpts{}
	cmd := smallCommand("delete <service-account-id>", "Delete a service account and its tokens")
	cmd.Args = cobra.ExactArgs(1)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := o.Validate(); err != nil {
			return err
		}
		id, err := positiveID(args[0])
		if err != nil {
			return err
		}
		result := cmdio.NewSingleMutation("deleted", accountTarget(id))
		result.DryRun = o.DryRun
		if o.DryRun {
			return o.IO.Encode(cmd.OutOrStdout(), result)
		}
		if err := confirmOrgDelete(cmd, o.Force, "Delete the service account and all its tokens?"); err != nil {
			return err
		}
		c, err := loadClient(cmd, loader)
		if err != nil {
			return err
		}
		if err := c.send(cmd.Context(), http.MethodDelete, accountPath(id), nil, nil); err != nil {
			return err
		}
		return o.IO.Encode(cmd.OutOrStdout(), result)
	}
	o.mutationFlags(cmd.Flags(), true)
	return cmd
}
func newTokenDeleteCommand(loader providers.GrafanaConfigLoader) *cobra.Command {
	o := &accountOpts{}
	cmd := smallCommand("delete-token <service-account-id> <token-id>", "Revoke a service account token")
	cmd.Args = cobra.ExactArgs(2)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := o.Validate(); err != nil {
			return err
		}
		accountID, err := positiveID(args[0])
		if err != nil {
			return err
		}
		tokenID, err := positiveID(args[1])
		if err != nil {
			return err
		}
		result := cmdio.NewSingleMutation("deleted", cmdio.MutationTarget{Kind: "ServiceAccountToken", ID: args[1], Namespace: args[0]})
		result.DryRun = o.DryRun
		if o.DryRun {
			return o.IO.Encode(cmd.OutOrStdout(), result)
		}
		if err := confirmOrgDelete(cmd, o.Force, "Revoke the service account token?"); err != nil {
			return err
		}
		c, err := loadClient(cmd, loader)
		if err != nil {
			return err
		}
		if err := c.send(cmd.Context(), http.MethodDelete, tokenPath(accountID, tokenID), nil, nil); err != nil {
			return err
		}
		return o.IO.Encode(cmd.OutOrStdout(), result)
	}
	o.mutationFlags(cmd.Flags(), true)
	return cmd
}
func newTokenCreateCommand(loader providers.GrafanaConfigLoader) *cobra.Command {
	o := &accountOpts{}
	cmd := smallCommand("create-token <service-account-id> <name>", "Create a token in a new private file")
	cmd.Args = cobra.ExactArgs(2)
	cmd.Long = "Create a service account token. Write its key and a newline to a new file with mode 0600. The file must not exist. Stdout contains a receipt, never the key. If the file write fails after creation, revoke the token. A dry run validates inputs locally and does not reserve the file."
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := o.Validate(); err != nil {
			return err
		}
		id, err := positiveID(args[0])
		if err != nil {
			return err
		}
		if strings.TrimSpace(args[1]) == "" {
			return errors.New("token name must not be empty")
		}
		if strings.TrimSpace(o.File) == "" {
			return errors.New("--output-file is required: use --output-file ./token.txt")
		}
		ttl, err := shared.ParseDuration(o.TTL)
		if err != nil || strings.TrimSpace(o.TTL) == "" || ttl < 0 || ttl%time.Second != 0 {
			return fmt.Errorf("invalid --ttl %q: use a non-negative duration in whole seconds, for example --ttl 24h; zero means no expiry", o.TTL)
		}
		receipt := cmdio.NewArtifactReceipt("created", "text")
		receipt.Files = append(receipt.Files, cmdio.ArtifactFile{Path: o.File, Kind: "ServiceAccountToken", Count: 1})
		if o.DryRun {
			receipt.Summary.Skipped = 1
			receipt.SkippedReasons = map[string]int{"dry run": 1}
			return o.IO.Encode(cmd.OutOrStdout(), receipt)
		}
		c, err := loadClient(cmd, loader)
		if err != nil {
			return err
		}
		// Reserve the path before creating a credential. O_EXCL also rejects symlinks.
		file, err := os.OpenFile(o.File, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return fmt.Errorf("reserve token file: %w", err)
		}
		keep := false
		defer func() {
			_ = file.Close()
			if !keep {
				_ = os.Remove(o.File)
			}
		}()
		var token tokenResponse
		if err := c.send(cmd.Context(), http.MethodPost, accountPath(id)+"/tokens", map[string]any{"name": args[1], "secondsToLive": int64(ttl / time.Second)}, &token); err != nil {
			return err
		}
		if token.ID <= 0 {
			return errors.New("token response has no valid ID; check service account tokens before retrying")
		}
		var writeErr error
		if token.Key == "" {
			writeErr = errors.New("token response has no key")
		} else {
			_, writeErr = file.WriteString(token.Key + "\n")
		}
		if writeErr == nil {
			writeErr = file.Sync()
		}
		if closeErr := file.Close(); writeErr == nil {
			writeErr = closeErr
		}
		if writeErr != nil {
			// Revocation must still run when the command context was cancelled.
			ctx, cancel := context.WithTimeout(context.WithoutCancel(cmd.Context()), 30*time.Second)
			defer cancel()
			revokeErr := c.send(ctx, http.MethodDelete, tokenPath(id, token.ID), nil, nil)
			if revokeErr != nil {
				return fmt.Errorf("token file failed; revoke token %d from service account %d before retrying: %w", token.ID, id, revokeErr)
			}
			return errors.New("token file failed; the new token was revoked")
		}
		keep = true
		receipt.Summary.Succeeded = 1
		return o.IO.Encode(cmd.OutOrStdout(), receipt)
	}
	o.IO.RegisterCustomCodec("table", orgTokenReceiptTable().Codec("table"))
	o.IO.DefaultFormat("table")
	o.setup(cmd.Flags())
	cmd.Flags().BoolVar(&o.DryRun, "dry-run", false, "Show the local plan without a write request or a file write")
	cmd.Flags().StringVar(&o.File, "output-file", "", "New file for the token key (required; existing files are rejected)")
	cmd.Flags().StringVar(&o.TTL, "ttl", "1h", "Token lifetime as a duration in whole seconds, for example 24h; zero means no expiry")
	return cmd
}
func newRolesCommand(loader providers.GrafanaConfigLoader) *cobra.Command {
	cmd := &cobra.Command{Use: "roles", Short: "Inspect RBAC roles and assign fixed roles to service accounts"}
	cmd.AddCommand(newRolesListCommand(loader), newRoleAssignCommand(loader))
	return cmd
}
func newRolesListCommand(loader providers.GrafanaConfigLoader) *cobra.Command {
	o := &accountOpts{}
	cmd := smallCommand("list", "List RBAC roles in the selected organization")
	cmd.Args = cobra.NoArgs
	cmd.Long = "Fetch all RBAC roles. Filter names locally by a case-sensitive substring. An empty filter selects all roles. This API requires Grafana Enterprise or Grafana Cloud RBAC."
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if err := o.Validate(); err != nil {
			return err
		}
		c, err := loadClient(cmd, loader)
		if err != nil {
			return err
		}
		roles, err := c.roles(cmd.Context())
		if err != nil {
			return err
		}
		selected := []Role{}
		for _, role := range roles {
			if strings.Contains(role.Name, o.Filter) {
				selected = append(selected, role)
			}
		}
		selected, meta := cmdio.TruncateCompleteList(selected, o.Limit)
		result := roleListResult{selected, cmdio.AttachListMeta(meta, os.Args)}
		if err := o.IO.Encode(cmd.OutOrStdout(), result); err != nil {
			return err
		}
		cmdio.EmitListTruncationHint(cmd.ErrOrStderr(), meta)
		return nil
	}
	o.IO.RegisterCustomCodec("table", roleListTable().Codec("table"))
	o.IO.DefaultFormat("table")
	o.setup(cmd.Flags())
	cmd.Flags().StringVar(&o.Filter, "filter", "", "Case-sensitive substring of the role name; empty selects all (client-side)")
	o.IO.BindListLimit(cmd.Flags(), &o.Limit, "roles", 50)
	return cmd
}
func newRoleAssignCommand(loader providers.GrafanaConfigLoader) *cobra.Command {
	o := &accountOpts{}
	cmd := smallCommand("assign <role-uid>", "Add one fixed RBAC role to a service account")
	cmd.Args = cobra.ExactArgs(1)
	cmd.Long = "Resolve the exact role UID from the role list and require a fixed: role name. Add the organization-scoped assignment to the service account. Existing role assignments remain. This does not change the basic organization role. A dry run reads roles but sends no write request. Use roles list --filter fixed:org.users:writer to find the user writer role."
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := o.Validate(); err != nil {
			return err
		}
		if strings.TrimSpace(args[0]) == "" {
			return errors.New("role UID must not be empty")
		}
		if o.AccountID <= 0 {
			return errors.New("--service-account-id requires a positive integer, for example --service-account-id 123")
		}
		c, err := loadClient(cmd, loader)
		if err != nil {
			return err
		}
		roles, err := c.roles(cmd.Context())
		if err != nil {
			return err
		}
		var found *Role
		for i := range roles {
			if roles[i].UID == args[0] {
				if found != nil {
					return errors.New("role UID is ambiguous")
				}
				found = &roles[i]
			}
		}
		if found == nil {
			return fmt.Errorf("role UID %q not found", args[0])
		}
		if !strings.HasPrefix(found.Name, "fixed:") {
			return fmt.Errorf("role %q is not a fixed role", found.Name)
		}
		result := cmdio.NewSingleMutation("assigned", cmdio.MutationTarget{Kind: "RoleAssignment", UID: args[0], Name: found.Name, ID: strconv.FormatInt(o.AccountID, 10)})
		result.DryRun = o.DryRun
		var account ServiceAccount
		if err := c.send(cmd.Context(), http.MethodGet, accountPath(o.AccountID), nil, &account); err != nil {
			return err
		}
		if account.ID != o.AccountID {
			return errors.New("service account response does not match the requested ID")
		}
		if !o.DryRun {
			path := "/api/access-control/users/" + strconv.FormatInt(o.AccountID, 10) + "/roles"
			if err := c.send(cmd.Context(), http.MethodPost, path, map[string]any{"roleUid": args[0], "global": false}, nil); err != nil {
				return err
			}
		}
		return o.IO.Encode(cmd.OutOrStdout(), result)
	}
	o.mutationFlags(cmd.Flags(), false)
	cmd.Flags().Int64Var(&o.AccountID, "service-account-id", 0, "Target service account ID (required; positive integer)")
	return cmd
}
