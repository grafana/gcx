package scim

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/native"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type usersOptions struct {
	IO                                                          cmdio.Options
	TokenFile, Filter, UserName, ExternalID, Email, DisplayName string
	DryRun, Force                                               bool
}

func (o *usersOptions) setup(flags *pflag.FlagSet, action string) {
	o.IO.DefaultFormat("yaml")
	if action == "list" {
		cmdio.RegisterTable(&o.IO, cmdio.Table[userResource]{Columns: []cmdio.Column[userResource]{
			{Header: "ID", Content: func(u userResource) string { value, _ := u["id"].(string); return value }},
			{Header: "USER NAME", Content: func(u userResource) string { value, _ := u["userName"].(string); return value }},
			{Header: "DISPLAY NAME", Content: func(u userResource) string { value, _ := u["displayName"].(string); return value }},
			{Header: "ACTIVE", Content: func(u userResource) string {
				value, ok := u["active"].(bool)
				if !ok {
					return ""
				}
				return strconv.FormatBool(value)
			}},
		}})
		o.IO.DefaultFormat("table")
	}
	if action == "create" || action == "update" || action == "deactivate" {
		o.IO.RegisterCustomCodec("text", userMutationCodec{})
		o.IO.DefaultFormat("text")
	}
	o.IO.BindFlags(flags)
	flags.StringVar(&o.TokenFile, "token-file", "", "Read the SCIM service account token from a file (- for standard input; required)")
	if action == "list" {
		flags.StringVar(&o.Filter, "filter", "", "SCIM filter expression sent to Grafana")
	}
	if action == "create" || action == "update" {
		flags.StringVar(&o.UserName, "user-name", "", "SCIM user name (required for create)")
		flags.StringVar(&o.ExternalID, "external-id", "", "Stable identity provider user ID (required for create)")
		flags.StringVar(&o.Email, "email", "", "Work email address")
		flags.StringVar(&o.DisplayName, "display-name", "", "User display name")
	}
	if action == "create" || action == "update" || action == "deactivate" {
		flags.BoolVar(&o.DryRun, "dry-run", false, "Show the proposed request without a write")
	}
	if action == "deactivate" {
		flags.BoolVar(&o.Force, "force", false, "Skip the deactivation confirmation")
	}
}

func (o *usersOptions) Validate(cmd *cobra.Command, action string, args []string) error {
	if strings.TrimSpace(o.TokenFile) == "" {
		return fail.NewCommandUsageError(cmd, "--token-file must name a file or -", nil)
	}
	if len(args) != 0 && (strings.TrimSpace(args[0]) == "" || args[0] == "." || args[0] == ".." || strings.ContainsAny(args[0], "/\\")) {
		return fail.NewCommandUsageError(cmd, "SCIM user ID must be a non-empty path segment", nil)
	}
	changed := false
	for _, name := range []string{"user-name", "external-id", "email", "display-name"} {
		if cmd.Flags().Changed(name) {
			changed = true
			value, _ := cmd.Flags().GetString(name)
			if strings.TrimSpace(value) == "" {
				return fail.NewCommandUsageError(cmd, "--"+name+" must not be empty", nil)
			}
		}
	}
	if action == "create" && (strings.TrimSpace(o.UserName) == "" || strings.TrimSpace(o.ExternalID) == "") {
		return fail.NewCommandUsageError(cmd, "create requires --user-name and --external-id", nil)
	}
	if action == "update" && !changed {
		return fail.NewCommandUsageError(cmd, "set at least one SCIM user attribute flag", nil)
	}
	if action == "list" && cmd.Flags().Changed("filter") && strings.TrimSpace(o.Filter) == "" {
		return fail.NewCommandUsageError(cmd, "--filter must not be empty", nil)
	}
	return o.IO.Validate()
}

func (o *usersOptions) token(cmd *cobra.Command) (string, error) {
	if o.TokenFile == "-" {
		return readUserToken(cmd.InOrStdin())
	}
	file, err := os.Open(o.TokenFile)
	if err != nil {
		return "", fmt.Errorf("open SCIM token file: %w", err)
	}
	token, readErr := readUserToken(file)
	closeErr := file.Close()
	if readErr != nil {
		return "", readErr
	}
	if closeErr != nil {
		return "", fmt.Errorf("close SCIM token file: %w", closeErr)
	}
	return token, nil
}

func newUsersCommand(binding native.Binding) *cobra.Command {
	cmd := &cobra.Command{Use: "users", Short: "Manage SCIM users", Long: "Manage users through the SCIM API with a service account token.\nUse organization user commands for organization membership.\nGrafana retains SCIM user records after deactivation."}
	for _, action := range []string{"list", "get", "create", "update", "deactivate"} {
		cmd.AddCommand(newUsersAction(binding, action))
	}
	return cmd
}

func newUsersAction(binding native.Binding, action string) *cobra.Command {
	o := &usersOptions{}
	use := action
	argsCheck := cobra.NoArgs
	if action == "get" || action == "update" || action == "deactivate" {
		use += " <id>"
		argsCheck = cobra.ExactArgs(1)
	}
	short := map[string]string{"list": "List all matching SCIM users", "get": "Get a SCIM user by ID", "create": "Create a SCIM user", "update": "Update selected SCIM user attributes", "deactivate": "Deactivate a SCIM user and keep its record"}
	cmd := &cobra.Command{Use: use, Short: short[action], Args: argsCheck, Example: "  gcx scim users " + strings.ReplaceAll(use, "<id>", "12345") + " --token-file ./scim-token", RunE: func(cmd *cobra.Command, args []string) error {
		if err := o.Validate(cmd, action, args); err != nil {
			return err
		}
		if action == "deactivate" && !o.DryRun {
			if err := o.confirmDeactivation(cmd, args[0]); err != nil {
				return err
			}
		}
		token, err := o.token(cmd)
		if err != nil {
			return err
		}
		client, err := loadUsersClient(cmd.Context(), binding, token)
		if err != nil {
			return err
		}
		if action == "list" {
			users, err := client.list(cmd.Context(), o.Filter)
			if err != nil {
				return err
			}
			return o.IO.Encode(cmd.OutOrStdout(), users)
		}
		if action == "get" {
			user, err := client.get(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return o.IO.Encode(cmd.OutOrStdout(), user)
		}
		body := o.body(cmd, action)
		mutationAction := map[string]string{"create": "created", "update": "updated", "deactivate": "deactivated"}[action]
		if o.DryRun {
			target := ""
			if len(args) != 0 {
				target = args[0]
			}
			result := newUserMutation(mutationAction, target, o.UserName)
			result.DryRun, result.Request = true, body
			return o.IO.Encode(cmd.OutOrStdout(), result)
		}
		method, suffix := http.MethodPost, ""
		if action != "create" {
			method, suffix = http.MethodPatch, "/"+url.PathEscape(args[0])
		}
		user, err := client.request(cmd.Context(), method, suffix, nil, body)
		if err != nil {
			subject := o.UserName
			if len(args) != 0 {
				subject = args[0]
			}
			return fmt.Errorf("%s SCIM user %q: %w", action, subject, err)
		}
		if user == nil {
			if action == "create" {
				return errors.New("SCIM create returned no user resource")
			}
			user, err = client.get(cmd.Context(), args[0])
			if err != nil {
				return fmt.Errorf("SCIM user change succeeded, but reading its result failed: %w", err)
			}
		}
		id, _ := user["id"].(string)
		if id == "" {
			return errors.New("SCIM user change succeeded, but the response has no user ID")
		}
		name, _ := user["userName"].(string)
		result := newUserMutation(mutationAction, id, name)
		result.User = user
		return o.IO.Encode(cmd.OutOrStdout(), result)
	}}
	if action == "create" {
		cmd.Long = "Create a SCIM user with a stable external ID. Grafana can link an existing account with the same email. The resulting SCIM record can be deactivated but cannot be deleted. Use --dry-run to show the request without a write."
	}
	if action == "list" {
		cmd.Long = "Fetch all SCIM user pages. Send --filter to Grafana as a SCIM expression. The command fails if it cannot verify that all matching users were returned."
	}
	if action == "update" {
		cmd.Long = "Replace only the user attributes named by flags. Keep other attributes unchanged. Use --dry-run to show the request without a write."
	}
	if action == "deactivate" {
		cmd.Long = "Set active to false. Grafana keeps the user record. Use --dry-run to show the request without a write. Use --force for a non-interactive write."
	}
	if action == "create" {
		cmd.Example = "  gcx scim users create --user-name test@example.com --external-id test-0001 --token-file ./scim-token --dry-run"
	}
	if action == "update" {
		cmd.Example = "  gcx scim users update 12345 --display-name 'Test User' --token-file ./scim-token --dry-run"
	}
	o.setup(cmd.Flags(), action)
	return cmd
}

func (o *usersOptions) body(cmd *cobra.Command, action string) map[string]any {
	values := map[string]any{}
	if action == "deactivate" {
		values["active"] = false
	}
	for _, attr := range []struct{ flag, key, value string }{{"user-name", "userName", o.UserName}, {"external-id", "externalId", o.ExternalID}, {"display-name", "displayName", o.DisplayName}} {
		if cmd.Flags().Changed(attr.flag) {
			values[attr.key] = attr.value
		}
	}
	if cmd.Flags().Changed("email") {
		values["emails"] = []map[string]any{{"value": o.Email, "type": "work", "primary": true}}
	}
	if action == "create" {
		values["schemas"] = []string{userSchema}
		values["active"] = true
		return values
	}
	// One pathless replace changes supplied attributes and keeps other attributes.
	return map[string]any{"schemas": []string{patchSchema}, "Operations": []map[string]any{{"op": "replace", "value": values}}}
}

// confirmDeactivation keeps the confirmation input separate from a standard input token.
func (o *usersOptions) confirmDeactivation(cmd *cobra.Command, id string) error {
	if o.TokenFile == "-" {
		bypass, err := providers.CheckDestructiveBypass(o.Force)
		if err != nil {
			return err
		}
		if !bypass {
			return errors.New("standard input contains the SCIM token; use --force to deactivate")
		}
	}
	proceed, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.ErrOrStderr(), o.Force, "Deactivate SCIM user "+id+"?")
	if err != nil {
		return err
	}
	if !proceed {
		exitCode := gcxerrors.ExitCancelled
		return &gcxerrors.DetailedError{Summary: "deactivation cancelled", ExitCode: &exitCode}
	}
	return nil
}
