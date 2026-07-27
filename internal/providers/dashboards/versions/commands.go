// Package versions provides the `gcx dashboards list-versions` command and
// the `gcx dashboards versions` command group.
package versions

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/dashboards/descriptor"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/dynamic"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
)

// DashboardVersionsClient is the subset of dynamic.NamespacedClient used by this package.
// Exported so that tests can implement the interface from the external test package.
type DashboardVersionsClient interface {
	List(ctx context.Context, desc resources.Descriptor, opts metav1.ListOptions) (*unstructured.UnstructuredList, error)
	Get(ctx context.Context, desc resources.Descriptor, name string, opts metav1.GetOptions) (*unstructured.Unstructured, error)
	Update(ctx context.Context, desc resources.Descriptor, obj *unstructured.Unstructured, opts metav1.UpdateOptions) (*unstructured.Unstructured, error)
}

// GrafanaConfigLoader is the subset of the config loader used here.
// Defined as a local interface so the command can be tested with a stub
// (narrow coupling; the concrete *providers.ConfigLoader satisfies the interface).
type GrafanaConfigLoader interface {
	LoadGrafanaConfig(ctx context.Context) (config.NamespacedRESTConfig, error)
}

// commandDeps holds either a real config loader (production path) or a pre-built
// client+descriptor (test path). Tests set client+desc; production sets loader.
type commandDeps struct {
	// Production: loader is used to build client and desc at runtime.
	loader GrafanaConfigLoader

	// Test overrides: if client is non-nil, client+desc are used directly
	// and loader is never called.
	client DashboardVersionsClient
	desc   resources.Descriptor
}

// resolve returns a client and descriptor, either from the pre-built test overrides
// or by calling descriptor.Resolve + dynamic.NewDefaultNamespacedClient.
func (d *commandDeps) resolve(ctx context.Context, apiVersion string) (DashboardVersionsClient, resources.Descriptor, error) {
	if d.client != nil {
		return d.client, d.desc, nil
	}

	cfg, err := d.loader.LoadGrafanaConfig(ctx)
	if err != nil {
		return nil, resources.Descriptor{}, err
	}

	desc, err := descriptor.Resolve(ctx, cfg, apiVersion)
	if err != nil {
		return nil, resources.Descriptor{}, err
	}

	client, err := dynamic.NewDefaultNamespacedClient(cfg)
	if err != nil {
		return nil, resources.Descriptor{}, err
	}

	return client, desc, nil
}

// Commands returns the versions subcommand group with the restore child.
func Commands(loader GrafanaConfigLoader) *cobra.Command {
	deps := &commandDeps{loader: loader}

	cmd := &cobra.Command{
		Use:   "versions",
		Short: "Manage dashboard version history",
	}
	cmd.AddCommand(newRestoreCommand(deps))
	return cmd
}

// ListVersionsCommand returns the `gcx dashboards list-versions` leaf command.
// Versions are addressed by the parent dashboard's name (a version has no ID
// of its own), so the command is an operation-subject compound mounted
// directly under `dashboards`.
func ListVersionsCommand(loader GrafanaConfigLoader) *cobra.Command {
	deps := &commandDeps{loader: loader}
	return newListVersionsCommand(deps)
}

// ---------------------------------------------------------------------------
// dashboards list-versions
// ---------------------------------------------------------------------------

type versionsListOpts struct {
	IO         cmdio.Options
	Limit      int64
	APIVersion string
}

func (o *versionsListOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &versionsTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)

	flags.Int64Var(&o.Limit, "limit", 0, "Maximum number of revisions to return (0 = all)")
	flags.StringVar(&o.APIVersion, "api-version", "", "API version to use (e.g. dashboard.grafana.app/v1); defaults to server preferred version")
}

func (o *versionsListOpts) Validate() error {
	return o.IO.Validate()
}

// newListVersionsCommand returns the `dashboards list-versions <name>` command.
//
// It issues a K8s LIST with magic selectors:
//
//	labelSelector=grafana.app/get-history=true
//	fieldSelector=metadata.name=<name>
//
// Results are sorted by descending metadata.generation before rendering.
// Supports -o table (default), json, yaml via cmdio.Options.
func newListVersionsCommand(deps *commandDeps) *cobra.Command {
	opts := &versionsListOpts{}

	cmd := &cobra.Command{
		Use:   "list-versions <name>",
		Short: "List dashboard version history",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			name := args[0]
			ctx := cmd.Context()

			client, desc, err := deps.resolve(ctx, opts.APIVersion)
			if err != nil {
				return err
			}

			listOpts := metav1.ListOptions{
				LabelSelector: "grafana.app/get-history=true",
				FieldSelector: fields.OneTermEqualSelector("metadata.name", name).String(),
			}
			if opts.Limit > 0 {
				listOpts.Limit = opts.Limit
			}

			list, err := client.List(ctx, desc, listOpts)
			if err != nil {
				return err
			}

			// Sort descending by generation (highest = most recent first).
			sort.SliceStable(list.Items, func(i, j int) bool {
				return list.Items[i].GetGeneration() > list.Items[j].GetGeneration()
			})

			return opts.IO.Encode(cmd.OutOrStdout(), list)
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}

// ---------------------------------------------------------------------------
// versions restore
// ---------------------------------------------------------------------------

type versionsRestoreOpts struct {
	IO         cmdio.Options
	Force      bool
	Message    string
	APIVersion string
}

func (o *versionsRestoreOpts) setup(flags *pflag.FlagSet) {
	// The restore result is a restoreResult document through the codec
	// system: the default text codec intentionally prints nothing (the
	// command's stdout has always been empty — prompt and success prose live
	// on stderr); agent mode and explicit -o json/yaml get the structured
	// document with the server-assigned new generation.
	o.IO.RegisterCustomCodec("text", &restoreTextCodec{})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)

	flags.BoolVar(&o.Force, "force", false, "Skip confirmation prompt")
	flags.StringVar(&o.Message, "message", "", `Commit message for the restored revision (default: "Restored from version N")`)
	flags.StringVar(&o.APIVersion, "api-version", "", "API version to use (e.g. dashboard.grafana.app/v1); defaults to server preferred version")
}

func (o *versionsRestoreOpts) Validate() error {
	return o.IO.Validate()
}

// restoreResult is the finite result document for `versions restore`. The
// domain needs fields the shared cmdio.SingleMutation cannot carry — the
// restored version and the server-assigned new generation — so it is a
// bespoke shape with the collision-resistant discriminators the mutation
// result family requires.
type restoreResult struct {
	Type          string               `json:"type" yaml:"type"`
	SchemaVersion string               `json:"schema_version" yaml:"schema_version"`
	Action        string               `json:"action" yaml:"action"`
	Target        cmdio.MutationTarget `json:"target" yaml:"target"`
	// Changed is false when the dashboard was already at the target version
	// (idempotent no-op: no PUT issued), true when a new generation was
	// written.
	Changed         bool  `json:"changed" yaml:"changed"`
	RestoredVersion int64 `json:"restored_version" yaml:"restored_version"`
	// NewGeneration is the dashboard generation after the command: the
	// server-assigned generation of the PUT response, or the current
	// generation for a no-op.
	NewGeneration int64 `json:"new_generation" yaml:"new_generation"`
}

// Discriminator values for the restore result shape.
const (
	restoreResultType          = "gcx.dashboards.restore"
	restoreResultSchemaVersion = "1"
)

// newRestoreResult returns a restoreResult with the discriminators set.
func newRestoreResult(desc resources.Descriptor, name string, changed bool, restoredVersion, newGeneration int64) restoreResult {
	return restoreResult{
		Type:            restoreResultType,
		SchemaVersion:   restoreResultSchemaVersion,
		Action:          "restored",
		Target:          cmdio.MutationTarget{Kind: desc.Kind, Name: name},
		Changed:         changed,
		RestoredVersion: restoredVersion,
		NewGeneration:   newGeneration,
	}
}

// restoreTextCodec is the human "text" codec for restoreResult values. The
// restore command has never written anything to stdout — its prompt, no-op
// note, and success line all live on stderr — so the codec renders nothing,
// keeping default human stdout byte-identical (empty).
type restoreTextCodec struct{}

func (c *restoreTextCodec) Format() format.Format { return "text" }

func (c *restoreTextCodec) Decode(io.Reader, any) error {
	return errors.New("text codec does not support decoding")
}

func (c *restoreTextCodec) Encode(_ io.Writer, value any) error {
	if _, ok := value.(restoreResult); !ok {
		return errors.New("invalid data type for restore text codec: expected restoreResult")
	}
	return nil
}

// newRestoreCommand returns the `dashboards versions restore <name> <version>` subcommand.
//
// Restore compound sequence:
//  1. Parse <version> as integer BEFORE any HTTP call.
//  2. LIST history with magic selectors → find item where generation == <version>.
//  3. GET current dashboard → capture resourceVersion and generation.
//  4. No-op: if current.generation == <version>, note on stderr, encode the
//     restoreResult (changed=false) through the codec, exit 0.
//  5. Prompt on stderr unless --force.
//  6. Deep copy current, replace spec with historical spec, set grafana.app/message.
//  7. PUT → 409 → non-zero exit; else success prose to stderr and the
//     restoreResult (changed=true, server-assigned new generation) through
//     the codec — nothing on default human stdout, one JSON document in
//     agent mode / -o json|yaml.
func newRestoreCommand(deps *commandDeps) *cobra.Command {
	opts := &versionsRestoreOpts{}

	cmd := &cobra.Command{
		Use:   "restore <name> <version>",
		Short: "Restore a dashboard to a previous version",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			name := args[0]

			// Parse version argument before any HTTP call.
			targetGen, err := strconv.ParseInt(args[1], 10, 64)
			if err != nil {
				return fmt.Errorf("version must be an integer, got %q: %w", args[1], err)
			}

			ctx := cmd.Context()

			client, desc, err := deps.resolve(ctx, opts.APIVersion)
			if err != nil {
				return err
			}

			// Step 2: LIST history to find the target revision.
			historyOpts := metav1.ListOptions{
				LabelSelector: "grafana.app/get-history=true",
				FieldSelector: fields.OneTermEqualSelector("metadata.name", name).String(),
			}

			historyList, err := client.List(ctx, desc, historyOpts)
			if err != nil {
				return err
			}

			var historicalItem *unstructured.Unstructured
			for i := range historyList.Items {
				if historyList.Items[i].GetGeneration() == targetGen {
					historicalItem = &historyList.Items[i]
					break
				}
			}

			if historicalItem == nil {
				return fmt.Errorf("version %d not found in the revision history of dashboard %q", targetGen, name)
			}

			// Step 3: GET current dashboard (capture resourceVersion + generation).
			current, err := client.Get(ctx, desc, name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			currentGen := current.GetGeneration()

			// Step 4: No-op if already at target version.
			if currentGen == targetGen {
				cmdio.Success(cmd.ErrOrStderr(), "already at version %d", targetGen)
				return opts.IO.Encode(cmd.OutOrStdout(),
					newRestoreResult(desc, name, false, targetGen, currentGen))
			}

			// Step 5: Prompt unless --force.
			proceed, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.ErrOrStderr(), opts.Force,
				fmt.Sprintf("Restore dashboard %q to version %d?", name, targetGen))
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			// Step 6: Construct update object.
			//
			// Deep copy current, then replace spec with historical spec.
			// Preserve all current metadata (name, namespace, uid, resourceVersion).
			// Set grafana.app/message on the annotations.
			obj := current.DeepCopy()

			// Extract historical spec.
			historicalSpec, found, err := unstructured.NestedFieldNoCopy(historicalItem.Object, "spec")
			if err != nil {
				return fmt.Errorf("failed to read historical spec: %w", err)
			}
			if !found {
				historicalSpec = map[string]any{}
			}

			if err := unstructured.SetNestedField(obj.Object, historicalSpec, "spec"); err != nil {
				return fmt.Errorf("failed to set spec on update object: %w", err)
			}

			// Set restore message annotation.
			restoreMsg := opts.Message
			if restoreMsg == "" {
				restoreMsg = fmt.Sprintf("Restored from version %d", targetGen)
			}

			ann := obj.GetAnnotations()
			if ann == nil {
				ann = make(map[string]string)
			}
			ann["grafana.app/message"] = restoreMsg
			obj.SetAnnotations(ann)

			// resourceVersion is already preserved from current.DeepCopy().

			// Step 7: PUT.
			updated, err := client.Update(ctx, desc, obj, metav1.UpdateOptions{})
			if err != nil {
				return err
			}

			newGen := updated.GetGeneration()
			cmdio.Success(cmd.ErrOrStderr(), "restored to version %d (new generation %d)", targetGen, newGen)
			return opts.IO.Encode(cmd.OutOrStdout(),
				newRestoreResult(desc, name, true, targetGen, newGen))
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}
