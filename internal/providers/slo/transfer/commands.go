// Package transfer preserves the deprecated SLO push/pull CLI contracts while
// delegating resource operations to the shared resources pipeline.
package transfer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/resources/local"
	"github.com/grafana/gcx/internal/resources/process"
	"github.com/grafana/gcx/internal/resources/remote"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"
)

// pushItemResult is one successfully processed file in a push batch.
type pushItemResult struct {
	Action string `json:"action" yaml:"action"` // created | updated | dry-run
	Name   string `json:"name" yaml:"name"`
	UUID   string `json:"uuid,omitempty" yaml:"uuid,omitempty"`
	File   string `json:"file,omitempty" yaml:"file,omitempty"`
}

// pushBatchResult preserves the finite result document for SLO push commands. It is
// bespoke rather than a cmdio.BatchMutation because per-item outcomes carry
// information a consumer cannot recover otherwise (the server-assigned UUID
// of created reports), so the shape carries its own discriminators.
type pushBatchResult struct {
	Type          string                  `json:"type" yaml:"type"`
	SchemaVersion string                  `json:"schema_version" yaml:"schema_version"`
	Action        string                  `json:"action" yaml:"action"`
	DryRun        bool                    `json:"dry_run,omitempty" yaml:"dry_run,omitempty"`
	Summary       cmdio.MutationSummary   `json:"summary" yaml:"summary"`
	Items         []pushItemResult        `json:"items" yaml:"items"`
	Failures      []cmdio.MutationFailure `json:"failures" yaml:"failures"`
}

func newPushBatchResult(dryRun bool) pushBatchResult {
	return pushBatchResult{
		Type:          "gcx.slo.push_batch",
		SchemaVersion: "1",
		Action:        "pushed",
		DryRun:        dryRun,
		Items:         []pushItemResult{},
		Failures:      []cmdio.MutationFailure{},
	}
}

// pushResultCodec is the human "text" codec for pushBatchResult values: it
// renders exactly the per-file lines push has always printed. Failures are
// not rendered here — they stay on stderr as diagnostics, matching the
// pre-codec behavior where failures never reached stdout.
type pushResultCodec struct{ label string }

func (c *pushResultCodec) Format() format.Format { return "text" }

func (c *pushResultCodec) Decode(io.Reader, any) error {
	return errors.New("text codec does not support decoding")
}

func (c *pushResultCodec) Encode(w io.Writer, v any) error {
	result, ok := v.(pushBatchResult)
	if !ok {
		return errors.New("invalid data type for push result codec: expected pushBatchResult")
	}
	for _, item := range result.Items {
		switch item.Action {
		case "dry-run":
			cmdio.Info(w, "[dry-run] Would push %s %q (uuid=%s)", c.label, item.Name, item.UUID)
		case "created":
			cmdio.Success(w, "Created %s (uuid=%s)", item.Name, item.UUID)
		case "updated":
			cmdio.Success(w, "Updated %s", item.Name)
		}
	}
	return nil
}

type pushOpts struct {
	IO     cmdio.Options
	DryRun bool
}

func (o *pushOpts) setup(flags *pflag.FlagSet, label string) {
	flags.BoolVar(&o.DryRun, "dry-run", false, "Preview changes without making them")
	o.IO.RegisterCustomCodec("text", &pushResultCodec{label: label})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
}

func (o *pushOpts) Validate() error { return o.IO.Validate() }

type pullOpts struct{ OutputDir string }

func (o *pullOpts) setup(flags *pflag.FlagSet, label string) {
	flags.StringVarP(&o.OutputDir, "output-dir", "d", ".", fmt.Sprintf("Directory to write %s to", label))
}

// PushCommand is the legacy file-at-a-time front end to remote.Pusher.
// It retains the released result document and local-only dry-run preview.
func PushCommand[T adapter.ResourceNamer](binding providers.BoundResource[T], label, selector string) *cobra.Command {
	opts := &pushOpts{}
	cmd := &cobra.Command{
		Use: "push FILE...", Args: cobra.MinimumNArgs(1),
		Short: fmt.Sprintf("Push %s from files (Deprecated: use gcx resources push).", label),
		Long: fmt.Sprintf(`Push %s from files.

Deprecated: use gcx resources push %s -p PATH instead.
Writes update only an existing metadata.name UUID; an absent or unknown UUID creates a new resource.
Manifests may omit apiVersion and kind; this command supplies its resource type.
This compatibility command retains its file-at-a-time results and local-only --dry-run preview.
The preview shows manifest identities only; it does not resolve the remote UUID or determine create versus update.`, label, selector),
		RunE: func(cmd *cobra.Command, paths []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			crud, cfg, err := binding.Load(ctx)
			if err != nil {
				return err
			}
			client, registry := pipeline(crud)
			tracked := &pushClient{PushClient: client, label: label}
			pusher := remote.NewPusher(tracked, registry)
			result := newPushBatchResult(opts.DryRun)
			fail := func(file string, remaining int, target cmdio.MutationTarget, cause error) error {
				result.Summary.Failed++
				result.Summary.Skipped = remaining
				result.Failures = append(result.Failures, cmdio.MutationFailure{Target: target, Error: cause.Error()})
				if result.Summary.Succeeded == 0 {
					return fmt.Errorf("%s: %w", file, cause)
				}
				cmdio.Error(cmd.ErrOrStderr(), "%v", cause)
				if err := opts.IO.Encode(cmd.OutOrStdout(), result); err != nil {
					return err
				}
				return gcxerrors.NewEmittedError(gcxerrors.ExitPartialFailure, cause)
			}
			for i, file := range paths {
				target := cmdio.MutationTarget{Kind: crud.Descriptor.Kind, Name: file}
				data, err := os.ReadFile(file)
				if err != nil {
					return fail(file, len(paths)-i-1, target, err)
				}
				// Legacy provider commands inferred omitted envelope fields.
				var manifest map[string]any
				if err := yaml.Unmarshal(data, &manifest); err != nil {
					return fail(file, len(paths)-i-1, target, err)
				}
				if manifest == nil {
					manifest = map[string]any{}
				}
				if manifest["apiVersion"] == nil || manifest["apiVersion"] == "" {
					manifest["apiVersion"] = crud.Descriptor.GroupVersion.String()
				}
				if manifest["kind"] == nil || manifest["kind"] == "" {
					manifest["kind"] = crud.Descriptor.Kind
				}
				data, err = json.Marshal(manifest)
				if err != nil {
					return fail(file, len(paths)-i-1, target, err)
				}
				items := resources.NewResources()
				reader := local.FSReader{Decoders: format.Codecs()}
				// Legacy FILE arguments accept YAML (including JSON) regardless of suffix.
				if err := reader.ReadBytes(ctx, items, data, "yaml"); err != nil {
					return fail(file, len(paths)-i-1, target, err)
				}
				item := items.AsList()[0]
				if !crud.Descriptor.Matches(item.GroupVersionKind()) {
					return fail(file, len(paths)-i-1, target, fmt.Errorf("expected %s, got %s", crud.Descriptor.GroupVersionKind(), item.GroupVersionKind()))
				}
				// Validate typed input even for the legacy local-only preview.
				if _, err := crud.FromUnstructured(&item.Object); err != nil {
					return fail(file, len(paths)-i-1, target, err)
				}
				name, _, _ := unstructured.NestedString(item.Object.Object, "spec", "name")
				target.Name, target.UID = name, item.Name()
				outcome := pushItemResult{Action: "dry-run", Name: name, UUID: item.Name(), File: file}
				if !opts.DryRun {
					_, err := pusher.Push(ctx, remote.PushRequest{
						Resources: items, MaxConcurrency: 1, StopOnError: true,
						NoPushFailureLog: true, IncludeManaged: true,
						Processors: []remote.Processor{process.NewNamespaceOverrider(cfg.Namespace)},
					})
					if err != nil {
						return fail(file, len(paths)-i-1, target, err)
					}
					outcome.Action, outcome.UUID = tracked.action, tracked.result.GetName()
				}
				result.Items = append(result.Items, outcome)
				result.Summary.Succeeded++
			}
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}
	opts.setup(cmd.Flags(), label)
	return cmd
}

// PullCommand delegates fetching and file writing to the resource pipeline,
// retaining the legacy Kind/name.yaml layout and artifact receipt.
func PullCommand[T adapter.ResourceNamer](binding providers.BoundResource[T], label, selector string) *cobra.Command {
	opts := &pullOpts{}
	cmd := &cobra.Command{
		Use:   "pull",
		Short: fmt.Sprintf("Pull %s to disk (Deprecated: use gcx resources pull).", label),
		Long: fmt.Sprintf(`Pull %s to disk.

Deprecated: use gcx resources pull %s -p PATH -o yaml instead.
This compatibility command retains its Kind/name.yaml layout.`, label, selector),
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			crud, _, err := binding.Load(ctx)
			if err != nil {
				return err
			}
			client, registry := pipeline(crud)
			items := resources.NewResources()
			_, err = remote.NewPuller(client, registry).Pull(ctx, remote.PullRequest{
				Resources: items, StopOnError: true,
			})
			if err != nil {
				return err
			}
			dir := filepath.Join(opts.OutputDir, crud.Descriptor.Kind)
			if err := os.MkdirAll(dir, 0755); err != nil {
				return err
			}
			writer := local.FSWriter{
				Path: dir, StopOnError: true, Encoder: format.NewYAMLCodec(),
				Namer: func(res *resources.Resource) (string, error) { return res.Name() + ".yaml", nil },
			}
			if err := writer.Write(ctx, items); err != nil {
				return err
			}
			receipt := cmdio.NewArtifactReceipt("pulled", "yaml")
			receipt.Dir = dir
			receipt.Summary.Succeeded = items.Len()
			if items.Len() > 0 {
				receipt.Files = append(receipt.Files, cmdio.ArtifactFile{Path: dir, Kind: crud.Descriptor.Kind, Count: items.Len()})
			}
			return cmdio.EmitArtifactResult(cmd.OutOrStdout(), receipt, func(w io.Writer) error {
				cmdio.Success(w, "Pulled %d %s to %s/", items.Len(), label, dir)
				return nil
			})
		},
	}
	opts.setup(cmd.Flags(), label)
	return cmd
}

type resourceRegistry struct{ descriptor resources.Descriptor }

func (r resourceRegistry) SupportedResources() resources.Descriptors {
	return resources.Descriptors{r.descriptor}
}
func (r resourceRegistry) PreferredResources() resources.Descriptors { return r.SupportedResources() }

func pipeline[T adapter.ResourceNamer](crud *adapter.TypedCRUD[T]) (*adapter.ResourceClientRouter, resourceRegistry) {
	factory := func(context.Context) (adapter.ResourceAdapter, error) { return crud.AsAdapter(), nil }
	return adapter.NewResourceClientRouter(nil, map[schema.GroupVersionKind]adapter.Factory{
		crud.Descriptor.GroupVersionKind(): factory,
	}), resourceRegistry{crud.Descriptor}
}

// pushClient captures the successful pipeline write for the legacy receipt.
// Exposing only PushClient deliberately excludes the optional PushLister,
// preserving UUID-only upsert in legacy commands without changing generic pushes.
type pushClient struct {
	remote.PushClient

	label  string
	action string
	result *unstructured.Unstructured
}

func (c *pushClient) Create(ctx context.Context, desc resources.Descriptor, obj *unstructured.Unstructured, opts metav1.CreateOptions) (*unstructured.Unstructured, error) {
	result, err := c.PushClient.Create(ctx, desc, obj, opts)
	if err != nil {
		name, _, _ := unstructured.NestedString(obj.Object, "spec", "name")
		return nil, fmt.Errorf("failed to create %s %s: %w", c.label, name, err)
	}
	c.action, c.result = "created", result
	return result, err
}
func (c *pushClient) Update(ctx context.Context, desc resources.Descriptor, obj *unstructured.Unstructured, opts metav1.UpdateOptions) (*unstructured.Unstructured, error) {
	result, err := c.PushClient.Update(ctx, desc, obj, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to update %s %s: %w", c.label, obj.GetName(), err)
	}
	c.action, c.result = "updated", result
	return result, err
}

// Get preserves legacy lookup diagnostics while letting NotFound reach the pusher.
func (c *pushClient) Get(ctx context.Context, desc resources.Descriptor, name string, opts metav1.GetOptions) (*unstructured.Unstructured, error) {
	result, err := c.PushClient.Get(ctx, desc, name, opts)
	if err != nil && !apierrors.IsNotFound(err) {
		return nil, fmt.Errorf("failed to check %s %s: %w", c.label, name, err)
	}
	return result, err
}
