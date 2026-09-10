package k6

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func loadV6CommandAPI(ctx context.Context, loader CloudConfigLoader) (API, error) {
	client, _, err := authenticatedClient(ctx, loader)
	if err != nil {
		return nil, err
	}
	return client, nil
}

func parsePositiveID(value, subject string) (int, error) {
	id, err := strconv.Atoi(value)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid %s ID %q: expected a positive integer", subject, value)
	}
	return id, nil
}

func readRequest[T any](cmd *cobra.Command, filename string) (T, error) {
	var result T
	if strings.TrimSpace(filename) == "" {
		return result, errors.New("--filename/-f is required")
	}
	data, err := readFileOrStdin(cmd, filename)
	if err != nil {
		return result, fmt.Errorf("read %q: %w", filename, err)
	}
	if err := decodeYAMLOrJSON(data, &result); err != nil {
		return result, fmt.Errorf("parse %q: %w", filename, err)
	}
	return result, nil
}

func setupDataOutput(opts *cmdio.Options, flags *pflag.FlagSet, codec format.Codec) {
	opts.RegisterCustomCodec("table", codec)
	opts.DefaultFormat("table")
	opts.BindFlags(flags)
}

func setupMutationOutput(opts *cmdio.Options, flags *pflag.FlagSet, line func(cmdio.SingleMutation) string) {
	opts.RegisterCustomCodec("text", singleMutationTextCodec(line))
	opts.DefaultFormat("text")
	opts.BindFlags(flags)
}

func mutation(kind, id, action string) cmdio.SingleMutation {
	return cmdio.NewSingleMutation(action, cmdio.MutationTarget{Kind: kind, ID: id})
}

// --- auth validate ---

func newAuthValidateCommand(loader CloudConfigLoader) *cobra.Command {
	opts := &cmdio.Options{}
	cmd := &cobra.Command{Use: "validate", Short: "Validate k6 Cloud access for the selected stack.", Args: cobra.NoArgs}
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if err := opts.Validate(); err != nil {
			return err
		}
		api, err := loadV6CommandAPI(cmd.Context(), loader)
		if err != nil {
			return err
		}
		result, err := api.ValidateCloudAuth(cmd.Context())
		if err != nil {
			return err
		}
		return opts.Encode(cmd.OutOrStdout(), result)
	}
	setupDataOutput(opts, cmd.Flags(), authValidationTableCodec{})
	return cmd
}

// --- label keys ---

func newLabelKeysCommand(loader CloudConfigLoader) *cobra.Command {
	cmd := &cobra.Command{Use: "label-keys", Short: "Manage k6 Cloud label keys."}
	cmd.AddCommand(newLabelKeysListCommand(loader), newLabelKeysCreateCommand(loader), newLabelKeysUpdateCommand(loader), newLabelKeysDeleteCommand(loader))
	return cmd
}

func newLabelKeysListCommand(loader CloudConfigLoader) *cobra.Command {
	opts := &cmdio.Options{}
	cmd := &cobra.Command{Use: "list", Short: "List k6 Cloud label keys.", Args: cobra.NoArgs}
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if err := opts.Validate(); err != nil {
			return err
		}
		api, err := loadV6CommandAPI(cmd.Context(), loader)
		if err != nil {
			return err
		}
		result, err := api.ListLabelKeys(cmd.Context())
		if err != nil {
			return err
		}
		return opts.Encode(cmd.OutOrStdout(), result)
	}
	setupDataOutput(opts, cmd.Flags(), labelKeyTableCodec{})
	return cmd
}

func validLabelPart(value string) bool {
	if len(value) < 1 || len(value) > 64 {
		return false
	}
	return !strings.ContainsAny(value, "'\";\\%") && !strings.ContainsFunc(value, unicode.IsSpace)
}

func validateLabelKeyCreate(req LabelKeyCreateRequest) error {
	if len(req.Value) < 1 || len(req.Value) > 100 {
		return fmt.Errorf("label key count %d is invalid: expected 1 to 100", len(req.Value))
	}
	seen := map[string]struct{}{}
	for i, item := range req.Value {
		if !validLabelPart(item.Key) {
			return fmt.Errorf("label key %d %q is invalid: expected 1 to 64 characters without whitespace or '\";\\%%", i, item.Key)
		}
		if _, ok := seen[item.Key]; ok {
			return fmt.Errorf("label key %q is duplicated", item.Key)
		}
		seen[item.Key] = struct{}{}
		if item.Description != nil && (len(*item.Description) < 1 || len(*item.Description) > 255) {
			return fmt.Errorf("description for label key %q must contain 1 to 255 characters", item.Key)
		}
	}
	return nil
}

func newLabelKeysCreateCommand(loader CloudConfigLoader) *cobra.Command {
	type options struct {
		IO   cmdio.Options
		File string
	}
	opts := &options{}
	cmd := &cobra.Command{Use: "create -f <file>", Short: "Create k6 Cloud label keys.", Args: cobra.NoArgs}
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if err := opts.IO.Validate(); err != nil {
			return err
		}
		req, err := readRequest[LabelKeyCreateRequest](cmd, opts.File)
		if err != nil {
			return err
		}
		if err := validateLabelKeyCreate(req); err != nil {
			return err
		}
		api, err := loadV6CommandAPI(cmd.Context(), loader)
		if err != nil {
			return err
		}
		result, err := api.CreateLabelKeys(cmd.Context(), req)
		if err != nil {
			return err
		}
		return opts.IO.Encode(cmd.OutOrStdout(), result)
	}
	setupDataOutput(&opts.IO, cmd.Flags(), labelKeyTableCodec{})
	cmd.Flags().StringVarP(&opts.File, "filename", "f", "", "YAML or JSON request file (use - for stdin)")
	return cmd
}

func validateLabelKeyPatch(req LabelKeyPatch) error {
	if len(req) == 0 {
		return errors.New("label key update is empty: set key or description")
	}
	for field, value := range req {
		switch field {
		case "key":
			if value != nil && !validLabelPart(*value) {
				return fmt.Errorf("label key %q is invalid: expected 1 to 64 characters without whitespace or '\";\\%%", *value)
			}
		case "description":
			if value != nil && (len(*value) < 1 || len(*value) > 255) {
				return errors.New("label key description must contain 1 to 255 characters or be null")
			}
		default:
			return fmt.Errorf("unknown label key field %q: expected key or description", field)
		}
	}
	return nil
}

func newLabelKeysUpdateCommand(loader CloudConfigLoader) *cobra.Command {
	type options struct {
		IO   cmdio.Options
		File string
	}
	opts := &options{}
	cmd := &cobra.Command{Use: "update <id> -f <file>", Short: "Update a k6 Cloud label key.", Args: cobra.ExactArgs(1)}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := opts.IO.Validate(); err != nil {
			return err
		}
		id, err := parsePositiveID(args[0], "label key")
		if err != nil {
			return err
		}
		req, err := readRequest[LabelKeyPatch](cmd, opts.File)
		if err != nil {
			return err
		}
		if err := validateLabelKeyPatch(req); err != nil {
			return err
		}
		api, err := loadV6CommandAPI(cmd.Context(), loader)
		if err != nil {
			return err
		}
		result, err := api.UpdateLabelKey(cmd.Context(), id, req)
		if err != nil {
			return err
		}
		return opts.IO.Encode(cmd.OutOrStdout(), result)
	}
	setupDataOutput(&opts.IO, cmd.Flags(), labelKeyTableCodec{})
	cmd.Flags().StringVarP(&opts.File, "filename", "f", "", "YAML or JSON patch file (use - for stdin)")
	return cmd
}

func newLabelKeysDeleteCommand(loader CloudConfigLoader) *cobra.Command {
	type options struct {
		IO    cmdio.Options
		Force bool
	}
	opts := &options{}
	cmd := &cobra.Command{Use: "delete <id>", Short: "Delete a k6 Cloud label key.", Args: cobra.ExactArgs(1)}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := opts.IO.Validate(); err != nil {
			return err
		}
		id, err := parsePositiveID(args[0], "label key")
		if err != nil {
			return err
		}
		proceed, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.ErrOrStderr(), opts.Force, fmt.Sprintf("Delete label key %d?", id))
		if err != nil || !proceed {
			return err
		}
		api, err := loadV6CommandAPI(cmd.Context(), loader)
		if err != nil {
			return err
		}
		if err := api.DeleteLabelKey(cmd.Context(), id); err != nil {
			return err
		}
		return opts.IO.Encode(cmd.OutOrStdout(), mutation("label-key", strconv.Itoa(id), "deleted"))
	}
	setupMutationOutput(&opts.IO, cmd.Flags(), func(m cmdio.SingleMutation) string { return "Deleted label key " + m.Target.ID })
	cmd.Flags().BoolVar(&opts.Force, "force", false, "Delete without confirmation")
	return cmd
}

// --- load tests and options ---

func newLoadTestsMoveCommand(loader CloudConfigLoader) *cobra.Command {
	type options struct {
		IO        cmdio.Options
		ProjectID int
	}
	opts := &options{}
	cmd := &cobra.Command{Use: "move <load-test-id>", Short: "Move a k6 load test to another project.", Args: cobra.ExactArgs(1)}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := opts.IO.Validate(); err != nil {
			return err
		}
		id, err := parsePositiveID(args[0], "load test")
		if err != nil {
			return err
		}
		if opts.ProjectID <= 0 {
			return fmt.Errorf("invalid --project-id %d: expected a positive integer", opts.ProjectID)
		}
		api, err := loadV6CommandAPI(cmd.Context(), loader)
		if err != nil {
			return err
		}
		if err := api.MoveLoadTest(cmd.Context(), id, opts.ProjectID); err != nil {
			return err
		}
		return opts.IO.Encode(cmd.OutOrStdout(), mutation("load-test", strconv.Itoa(id), "moved"))
	}
	setupMutationOutput(&opts.IO, cmd.Flags(), func(m cmdio.SingleMutation) string { return "Moved load test " + m.Target.ID })
	cmd.Flags().IntVar(&opts.ProjectID, "project-id", 0, "Destination project ID (required)")
	return cmd
}

func newLoadTestsStartCommand(loader CloudConfigLoader) *cobra.Command {
	type options struct {
		IO  cmdio.Options
		Key string
	}
	opts := &options{}
	cmd := &cobra.Command{Use: "start <load-test-id>", Short: "Start a saved k6 Cloud load test.", Long: "Start a saved k6 Cloud load test. This operation can consume billable VUh.", Args: cobra.ExactArgs(1)}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := opts.IO.Validate(); err != nil {
			return err
		}
		id, err := parsePositiveID(args[0], "load test")
		if err != nil {
			return err
		}
		if cmd.Flags().Changed("idempotency-key") && (len(opts.Key) < 1 || len(opts.Key) > 36) {
			return fmt.Errorf("invalid --idempotency-key length %d: expected 1 to 36 characters", len(opts.Key))
		}
		api, err := loadV6CommandAPI(cmd.Context(), loader)
		if err != nil {
			return err
		}
		result, err := api.StartLoadTest(cmd.Context(), id, opts.Key)
		if err != nil {
			return err
		}
		return opts.IO.Encode(cmd.OutOrStdout(), result)
	}
	setupDataOutput(&opts.IO, cmd.Flags(), testRunTableCodec{})
	cmd.Flags().StringVar(&opts.Key, "idempotency-key", "", "Idempotency key, 1 to 36 characters")
	return cmd
}

func newLoadTestsGetScheduleCommand(loader CloudConfigLoader) *cobra.Command {
	opts := &cmdio.Options{}
	cmd := &cobra.Command{Use: "get-schedule <load-test-id>", Short: "Get the schedule for a k6 load test.", Args: cobra.ExactArgs(1)}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := opts.Validate(); err != nil {
			return err
		}
		id, err := parsePositiveID(args[0], "load test")
		if err != nil {
			return err
		}
		api, err := loadV6CommandAPI(cmd.Context(), loader)
		if err != nil {
			return err
		}
		result, err := api.GetLoadTestSchedule(cmd.Context(), id)
		if err != nil {
			return err
		}
		return opts.Encode(cmd.OutOrStdout(), result)
	}
	setupDataOutput(opts, cmd.Flags(), scheduleSingleTableCodec{})
	return cmd
}

func scriptAccept(kind string) (string, error) {
	switch kind {
	case "auto":
		return "*/*", nil
	case "javascript":
		return "text/javascript", nil
	case "archive":
		return "application/x-tar", nil
	default:
		return "", fmt.Errorf("invalid --type %q: expected auto, javascript, or archive", kind)
	}
}

func newRawScriptCommand(use, short string, loader CloudConfigLoader, download func(context.Context, API, int, string) (*ScriptDownload, error)) *cobra.Command {
	var kind string
	cmd := &cobra.Command{Use: use, Short: short, Args: cobra.ExactArgs(1)}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		id, err := parsePositiveID(args[0], "resource")
		if err != nil {
			return err
		}
		accept, err := scriptAccept(kind)
		if err != nil {
			return err
		}
		api, err := loadV6CommandAPI(cmd.Context(), loader)
		if err != nil {
			return err
		}
		result, err := download(cmd.Context(), api, id, accept)
		if err != nil {
			return err
		}
		_, err = cmd.OutOrStdout().Write(result.Data)
		return err
	}
	cmd.Flags().StringVar(&kind, "type", "auto", "Script type: auto, javascript, or archive")
	return cmd
}

func newLoadTestsGetScriptCommand(loader CloudConfigLoader) *cobra.Command {
	return newRawScriptCommand("get-script <load-test-id>", "Download a k6 load-test script.", loader, func(ctx context.Context, api API, id int, accept string) (*ScriptDownload, error) {
		return api.DownloadLoadTestScript(ctx, id, accept)
	})
}

func newOptionsCommand(loader CloudConfigLoader) *cobra.Command {
	cmd := &cobra.Command{Use: "options", Short: "Work with k6 Cloud test options."}
	cmd.AddCommand(newOptionsValidateCommand(loader))
	return cmd
}

func newOptionsValidateCommand(loader CloudConfigLoader) *cobra.Command {
	type options struct {
		IO   cmdio.Options
		File string
	}
	opts := &options{}
	cmd := &cobra.Command{Use: "validate -f <file>", Short: "Validate k6 Cloud test options and estimate VUh.", Args: cobra.NoArgs}
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if err := opts.IO.Validate(); err != nil {
			return err
		}
		req, err := readRequest[ValidateOptionsRequest](cmd, opts.File)
		if err != nil {
			return err
		}
		if req.Options == nil {
			return errors.New("options is required in the request file")
		}
		api, err := loadV6CommandAPI(cmd.Context(), loader)
		if err != nil {
			return err
		}
		result, err := api.ValidateTestOptions(cmd.Context(), req)
		if err != nil {
			return err
		}
		return opts.IO.Encode(cmd.OutOrStdout(), result)
	}
	setupDataOutput(&opts.IO, cmd.Flags(), validateOptionsTableCodec{})
	cmd.Flags().StringVarP(&opts.File, "filename", "f", "", "YAML or JSON request file (use - for stdin)")
	return cmd
}

// --- project limits and labels ---

func newProjectLimitsCommand(loader CloudConfigLoader) *cobra.Command {
	cmd := &cobra.Command{Use: "project-limits", Short: "Inspect k6 Cloud project limits."}
	cmd.AddCommand(newProjectLimitsListCommand(loader))
	return cmd
}

func newProjectLimitsListCommand(loader CloudConfigLoader) *cobra.Command {
	type options struct {
		IO         cmdio.Options
		ProjectIDs []int
		Limit      int
	}
	opts := &options{}
	cmd := &cobra.Command{Use: "list", Short: "List k6 Cloud project limits.", Args: cobra.NoArgs}
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if err := opts.IO.Validate(); err != nil {
			return err
		}
		if opts.Limit < 1 || opts.Limit > 1000 {
			return fmt.Errorf("invalid --limit %d: expected 1 to 1000", opts.Limit)
		}
		if len(opts.ProjectIDs) > 30 {
			return fmt.Errorf("too many --project-id values: expected at most 30, got %d", len(opts.ProjectIDs))
		}
		for _, id := range opts.ProjectIDs {
			if id <= 0 {
				return fmt.Errorf("invalid --project-id %d: expected a positive integer", id)
			}
		}
		api, err := loadV6CommandAPI(cmd.Context(), loader)
		if err != nil {
			return err
		}
		result, err := api.ListProjectLimits(cmd.Context(), opts.ProjectIDs, opts.Limit)
		if err != nil {
			return err
		}
		return opts.IO.Encode(cmd.OutOrStdout(), result)
	}
	setupDataOutput(&opts.IO, cmd.Flags(), projectLimitsTableCodec{})
	cmd.Flags().IntSliceVar(&opts.ProjectIDs, "project-id", nil, "Project IDs to include (maximum 30)")
	cmd.Flags().IntVar(&opts.Limit, "limit", 50, "Maximum number of projects to return (1 to 1000)")
	return cmd
}

func newProjectsGetLimitsCommand(loader CloudConfigLoader) *cobra.Command {
	opts := &cmdio.Options{}
	cmd := &cobra.Command{Use: "get-limits <project-id>", Short: "Get limits for a k6 Cloud project.", Args: cobra.ExactArgs(1)}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := opts.Validate(); err != nil {
			return err
		}
		id, err := parsePositiveID(args[0], "project")
		if err != nil {
			return err
		}
		api, err := loadV6CommandAPI(cmd.Context(), loader)
		if err != nil {
			return err
		}
		result, err := api.GetProjectLimits(cmd.Context(), id)
		if err != nil {
			return err
		}
		return opts.Encode(cmd.OutOrStdout(), result)
	}
	setupDataOutput(opts, cmd.Flags(), projectLimitsTableCodec{})
	return cmd
}

func validateProjectLimitsPatch(patch ProjectLimitsPatch) error {
	if len(patch) == 0 {
		return errors.New("project limits update is empty")
	}
	allowed := map[string]bool{"vuh_max_per_month": true, "vu_max_per_test": true, "vu_browser_max_per_test": true, "duration_max_per_test": true}
	for field, value := range patch {
		if !allowed[field] {
			return fmt.Errorf("unknown project limit field %q", field)
		}
		if value != nil && *value < 1 {
			return fmt.Errorf("project limit %s must be null or at least 1", field)
		}
	}
	return nil
}

func newProjectsUpdateLimitsCommand(loader CloudConfigLoader) *cobra.Command {
	type options struct {
		IO   cmdio.Options
		File string
	}
	opts := &options{}
	cmd := &cobra.Command{Use: "update-limits <project-id> -f <file>", Short: "Update limits for a k6 Cloud project.", Args: cobra.ExactArgs(1)}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := opts.IO.Validate(); err != nil {
			return err
		}
		id, err := parsePositiveID(args[0], "project")
		if err != nil {
			return err
		}
		patch, err := readRequest[ProjectLimitsPatch](cmd, opts.File)
		if err != nil {
			return err
		}
		if err := validateProjectLimitsPatch(patch); err != nil {
			return err
		}
		api, err := loadV6CommandAPI(cmd.Context(), loader)
		if err != nil {
			return err
		}
		if err := api.UpdateProjectLimits(cmd.Context(), id, patch); err != nil {
			return err
		}
		return opts.IO.Encode(cmd.OutOrStdout(), mutation("project", strconv.Itoa(id), "updated-limits"))
	}
	setupMutationOutput(&opts.IO, cmd.Flags(), func(m cmdio.SingleMutation) string { return "Updated limits for project " + m.Target.ID })
	cmd.Flags().StringVarP(&opts.File, "filename", "f", "", "YAML or JSON patch file (use - for stdin)")
	return cmd
}

func newProjectsListLabelsCommand(loader CloudConfigLoader) *cobra.Command {
	opts := &cmdio.Options{}
	cmd := &cobra.Command{Use: "list-labels <project-id>", Short: "List labels for a k6 Cloud project.", Args: cobra.ExactArgs(1)}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := opts.Validate(); err != nil {
			return err
		}
		id, err := parsePositiveID(args[0], "project")
		if err != nil {
			return err
		}
		api, err := loadV6CommandAPI(cmd.Context(), loader)
		if err != nil {
			return err
		}
		result, err := api.ListProjectLabels(cmd.Context(), id)
		if err != nil {
			return err
		}
		return opts.Encode(cmd.OutOrStdout(), result)
	}
	setupDataOutput(opts, cmd.Flags(), projectLabelTableCodec{})
	return cmd
}

func validateProjectLabels(req ProjectLabelPutRequest) error {
	if len(req.Value) > 30 {
		return fmt.Errorf("project label count %d is invalid: expected 0 to 30", len(req.Value))
	}
	for i, item := range req.Value {
		if (item.KeyID == nil) == (item.Key == nil) {
			return fmt.Errorf("project label %d must set exactly one of key_id or key", i)
		}
		if item.KeyID != nil && *item.KeyID <= 0 {
			return fmt.Errorf("project label %d has invalid key_id %d: expected a positive integer", i, *item.KeyID)
		}
		if item.Key != nil && !validLabelPart(*item.Key) {
			return fmt.Errorf("project label %d has invalid key %q", i, *item.Key)
		}
		if !validLabelPart(item.Value) {
			return fmt.Errorf("project label %d has invalid value %q", i, item.Value)
		}
	}
	return nil
}

func newProjectsUpdateLabelsCommand(loader CloudConfigLoader) *cobra.Command {
	type options struct {
		IO    cmdio.Options
		File  string
		Force bool
	}
	opts := &options{}
	cmd := &cobra.Command{Use: "update-labels <project-id> -f <file>", Short: "Replace all labels for a k6 Cloud project.", Args: cobra.ExactArgs(1)}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := opts.IO.Validate(); err != nil {
			return err
		}
		id, err := parsePositiveID(args[0], "project")
		if err != nil {
			return err
		}
		req, err := readRequest[ProjectLabelPutRequest](cmd, opts.File)
		if err != nil {
			return err
		}
		if err := validateProjectLabels(req); err != nil {
			return err
		}
		proceed, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.ErrOrStderr(), opts.Force, fmt.Sprintf("Replace all labels for project %d?", id))
		if err != nil || !proceed {
			return err
		}
		api, err := loadV6CommandAPI(cmd.Context(), loader)
		if err != nil {
			return err
		}
		result, err := api.ReplaceProjectLabels(cmd.Context(), id, req)
		if err != nil {
			return err
		}
		return opts.IO.Encode(cmd.OutOrStdout(), result)
	}
	setupDataOutput(&opts.IO, cmd.Flags(), projectLabelTableCodec{})
	cmd.Flags().StringVarP(&opts.File, "filename", "f", "", "YAML or JSON replacement file (use - for stdin)")
	cmd.Flags().BoolVar(&opts.Force, "force", false, "Replace without confirmation")
	return cmd
}

// --- schedule actions ---

func titleAction(action string) string {
	if action == "" {
		return ""
	}
	return strings.ToUpper(action[:1]) + action[1:]
}

func newScheduleActionCommand(loader CloudConfigLoader, use, short, action string, destructive bool, call func(context.Context, API, int) error) *cobra.Command {
	type options struct {
		IO    cmdio.Options
		Force bool
	}
	opts := &options{}
	cmd := &cobra.Command{Use: use + " <schedule-id>", Short: short, Args: cobra.ExactArgs(1)}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := opts.IO.Validate(); err != nil {
			return err
		}
		id, err := parsePositiveID(args[0], "schedule")
		if err != nil {
			return err
		}
		if destructive {
			proceed, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.ErrOrStderr(), opts.Force, fmt.Sprintf("Delete schedule %d?", id))
			if err != nil || !proceed {
				return err
			}
		}
		api, err := loadV6CommandAPI(cmd.Context(), loader)
		if err != nil {
			return err
		}
		if err := call(cmd.Context(), api, id); err != nil {
			return err
		}
		return opts.IO.Encode(cmd.OutOrStdout(), mutation("schedule", strconv.Itoa(id), action))
	}
	setupMutationOutput(&opts.IO, cmd.Flags(), func(m cmdio.SingleMutation) string { return titleAction(action) + " schedule " + m.Target.ID })
	if destructive {
		cmd.Flags().BoolVar(&opts.Force, "force", false, "Delete without confirmation")
	}
	return cmd
}

func newSchedulesDeleteCommand(loader CloudConfigLoader) *cobra.Command {
	return newScheduleActionCommand(loader, "delete", "Delete a k6 Cloud schedule.", "deleted", true, func(ctx context.Context, api API, id int) error { return api.DeleteSchedule(ctx, id) })
}
func newSchedulesActivateCommand(loader CloudConfigLoader) *cobra.Command {
	return newScheduleActionCommand(loader, "activate", "Activate a k6 Cloud schedule.", "activated", false, func(ctx context.Context, api API, id int) error { return api.ActivateSchedule(ctx, id) })
}
func newSchedulesDeactivateCommand(loader CloudConfigLoader) *cobra.Command {
	return newScheduleActionCommand(loader, "deactivate", "Deactivate a k6 Cloud schedule.", "deactivated", false, func(ctx context.Context, api API, id int) error { return api.DeactivateSchedule(ctx, id) })
}

func newRunsGetCommand(loader CloudConfigLoader) *cobra.Command {
	opts := &cmdio.Options{}
	cmd := &cobra.Command{Use: "get <run-id>", Short: "Get a k6 Cloud test run.", Args: cobra.ExactArgs(1)}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := opts.Validate(); err != nil {
			return err
		}
		id, err := parsePositiveID(args[0], "run")
		if err != nil {
			return err
		}
		api, err := loadV6CommandAPI(cmd.Context(), loader)
		if err != nil {
			return err
		}
		result, err := api.GetTestRun(cmd.Context(), id)
		if err != nil {
			return err
		}
		return opts.Encode(cmd.OutOrStdout(), result)
	}
	setupDataOutput(opts, cmd.Flags(), testRunTableCodec{})
	return cmd
}

func newRunsUpdateCommand(loader CloudConfigLoader) *cobra.Command {
	type options struct {
		IO   cmdio.Options
		Note string
	}
	opts := &options{}
	cmd := &cobra.Command{Use: "update <run-id>", Short: "Update the note for a k6 Cloud test run.", Args: cobra.ExactArgs(1)}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := opts.IO.Validate(); err != nil {
			return err
		}
		id, err := parsePositiveID(args[0], "run")
		if err != nil {
			return err
		}
		if !cmd.Flags().Changed("note") {
			return errors.New("--note is required; use --note '' to clear it")
		}
		api, err := loadV6CommandAPI(cmd.Context(), loader)
		if err != nil {
			return err
		}
		if err := api.UpdateTestRun(cmd.Context(), id, opts.Note); err != nil {
			return err
		}
		return opts.IO.Encode(cmd.OutOrStdout(), mutation("test-run", strconv.Itoa(id), "updated"))
	}
	setupMutationOutput(&opts.IO, cmd.Flags(), func(m cmdio.SingleMutation) string { return "Updated test run " + m.Target.ID })
	cmd.Flags().StringVar(&opts.Note, "note", "", "New run note; an explicit empty value clears it")
	return cmd
}

func newRunActionCommand(loader CloudConfigLoader, use, short, action string, destructive bool, call func(context.Context, API, int) error) *cobra.Command {
	type options struct {
		IO    cmdio.Options
		Force bool
	}
	opts := &options{}
	cmd := &cobra.Command{Use: use + " <run-id>", Short: short, Args: cobra.ExactArgs(1)}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := opts.IO.Validate(); err != nil {
			return err
		}
		id, err := parsePositiveID(args[0], "run")
		if err != nil {
			return err
		}
		if destructive {
			proceed, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.ErrOrStderr(), opts.Force, fmt.Sprintf("%s test run %d?", titleAction(action), id))
			if err != nil || !proceed {
				return err
			}
		}
		api, err := loadV6CommandAPI(cmd.Context(), loader)
		if err != nil {
			return err
		}
		if err := call(cmd.Context(), api, id); err != nil {
			return err
		}
		return opts.IO.Encode(cmd.OutOrStdout(), mutation("test-run", strconv.Itoa(id), action))
	}
	setupMutationOutput(&opts.IO, cmd.Flags(), func(m cmdio.SingleMutation) string { return titleAction(action) + " test run " + m.Target.ID })
	if destructive {
		cmd.Flags().BoolVar(&opts.Force, "force", false, "Run without confirmation")
	}
	return cmd
}

func newRunsDeleteCommand(loader CloudConfigLoader) *cobra.Command {
	return newRunActionCommand(loader, "delete", "Delete a k6 Cloud test run.", "deleted", true, func(ctx context.Context, api API, id int) error { return api.DeleteTestRun(ctx, id) })
}
func newRunsAbortCommand(loader CloudConfigLoader) *cobra.Command {
	return newRunActionCommand(loader, "abort", "Abort a running k6 Cloud test run.", "aborted", true, func(ctx context.Context, api API, id int) error { return api.AbortTestRun(ctx, id) })
}
func newRunsStarCommand(loader CloudConfigLoader) *cobra.Command {
	return newRunActionCommand(loader, "star", "Star a k6 Cloud test run.", "starred", false, func(ctx context.Context, api API, id int) error { return api.StarTestRun(ctx, id) })
}
func newRunsUnstarCommand(loader CloudConfigLoader) *cobra.Command {
	return newRunActionCommand(loader, "unstar", "Unstar a k6 Cloud test run.", "unstarred", false, func(ctx context.Context, api API, id int) error { return api.UnstarTestRun(ctx, id) })
}

func newRunsGetDistributionCommand(loader CloudConfigLoader) *cobra.Command {
	opts := &cmdio.Options{}
	cmd := &cobra.Command{Use: "get-distribution <run-id>", Short: "Get distribution details for a k6 Cloud test run.", Args: cobra.ExactArgs(1)}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := opts.Validate(); err != nil {
			return err
		}
		id, err := parsePositiveID(args[0], "run")
		if err != nil {
			return err
		}
		api, err := loadV6CommandAPI(cmd.Context(), loader)
		if err != nil {
			return err
		}
		result, err := api.GetTestRunDistribution(cmd.Context(), id)
		if err != nil {
			return err
		}
		return opts.Encode(cmd.OutOrStdout(), result)
	}
	setupDataOutput(opts, cmd.Flags(), distributionTableCodec{})
	return cmd
}

func newRunsGetScriptCommand(loader CloudConfigLoader) *cobra.Command {
	return newRawScriptCommand("get-script <run-id>", "Download the script for a k6 Cloud test run.", loader, func(ctx context.Context, api API, id int, accept string) (*ScriptDownload, error) {
		return api.DownloadTestRunScript(ctx, id, accept)
	})
}
