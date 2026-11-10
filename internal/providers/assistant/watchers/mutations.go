package watchers

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/assistant/watcher"
	"github.com/grafana/gcx/internal/docs"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
)

type writeOpts struct {
	IO     cmdio.Options
	File   string
	Ref    string
	Update bool
}

func (o *writeOpts) setup(flags *pflag.FlagSet) {
	bindIO(&o.IO, flags)
	flags.StringVarP(&o.File, "file", "f", "", "Read one complete Watcher YAML or JSON manifest from FILE; use - for stdin")
}
func (o *writeOpts) Validate() error {
	if strings.TrimSpace(o.File) == "" {
		return errors.New("--file requires a nonempty manifest path; try gcx assistant watchers create -f watcher.yaml")
	}
	if o.Update && strings.TrimSpace(o.Ref) == "" {
		return errors.New("WATCHER must be a nonempty resource name or server ID; try gcx assistant watchers update checkout-health -f watcher.yaml")
	}
	return o.IO.Validate()
}
func newWriteCommand(loader *providers.ConfigLoader, update bool) *cobra.Command {
	opts := &writeOpts{Update: update}
	subject := ""
	verb, use, short, long, args := "create", "create", "Create an Assistant Watcher and request calibration.", "Create one inactive Watcher from a complete configuration manifest, then request its first background calibration without automatic activation. The result includes the saved resource and a separate calibration request outcome; acceptance does not mean checks are ready. If calibration is unavailable or the request fails, the Watcher is retained without retry or rollback; continue in Grafana. An existing exact title is an error; use update to change it. Omitted optional settings receive authoritative defaults. Secret inputs use fromEnv/fromFile or clear; preserve markers are rejected on create. Use gcx resources push --dry-run for client-side manifest validation without mutation. Use resources push to save configuration without requesting calibration."+watcherBillingNote, cobra.NoArgs
	if update {
		subject = " checkout-health"
		verb, use, short, long, args = "update", "update WATCHER", "Reconcile an Assistant Watcher's configuration.", "Replace the modeled configuration of the selected Watcher from a complete manifest. WATCHER accepts a resource name or server ID and is authoritative; a conflicting manifest ID is rejected. Omitted optional settings receive defaults, including disabled destinations and empty collections. Unchanged configuration sends no mutation. Calibration-owned content is preserved; meaningful settings outside the manifest model refuse mutation. Every changed definition sends the complete modeled configuration, including secret-only edits. Use gcx resources push --dry-run for client-side validation without mutation. This command does not activate monitoring or request calibration.", cobra.ExactArgs(1)
	}
	cmd := &cobra.Command{Use: use, Short: short, Long: long, Args: args, Example: "  gcx assistant watchers " + verb + subject + " -f watcher.yaml\n  gcx assistant watchers " + verb + subject + " -f watcher.json -o json\n  gcx assistant watchers " + verb + subject + " -f - -o yaml", RunE: func(cmd *cobra.Command, args []string) error {
		if update {
			opts.Ref = args[0]
		}
		if err := opts.Validate(); err != nil {
			return err
		}
		manifest, err := readManifest(cmd, opts.File)
		if err != nil {
			return err
		}
		for _, warning := range manifest.Warnings() {
			cmdio.EmitWarn(cmd.ErrOrStderr(), warning)
		}
		client, namespace, err := newClient(cmd, loader)
		if err != nil {
			return err
		}
		crud := watcher.NewTypedCRUDForClient(client, namespace)
		obj := &adapter.TypedObject[watcher.Watcher]{Spec: *manifest}
		obj.SetName(manifest.GetResourceName())
		var saved *adapter.TypedObject[watcher.Watcher]
		if update {
			saved, err = crud.Update(cmd.Context(), opts.Ref, obj)
		} else {
			saved, err = crud.Create(cmd.Context(), obj)
		}
		var partial *watcher.PartialCompletionError
		if err != nil {
			if !errors.As(err, &partial) {
				return err
			}
			if !update {
				return emitCreate(cmd, &opts.IO, client, crud, partial.Watcher, partial)
			}
			return emitMutation(cmd, &opts.IO, verb, partial.Watcher, partial)
		}
		if !update {
			return emitCreate(cmd, &opts.IO, client, crud, saved.Spec, nil)
		}
		return emitMutation(cmd, &opts.IO, verb, saved.Spec, nil)
	}}
	markExperimental(cmd)
	cmd.Annotations[agent.AnnotationTokenCost] = "small"
	if !update {
		cmd.Annotations[agent.AnnotationLLMHint] = "Saves the Watcher and requests calibration, which consumes Grafana Assistant tokens (" + docs.AssistantPricing + "). Use when the user asks to create a Watcher."
	}
	opts.setup(cmd.Flags())
	return cmd
}
func readManifest(cmd *cobra.Command, path string) (*watcher.Watcher, error) {
	src := cmd.InOrStdin()
	if path != "-" {
		file, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("read Watcher manifest: %w", err)
		}
		defer file.Close()
		src = file
	}
	decoder := yaml.NewYAMLOrJSONDecoder(src, 4096)
	var envelope unstructured.Unstructured
	if err := decoder.Decode(&envelope.Object); err != nil {
		return nil, fmt.Errorf("decode Watcher manifest: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("--file requires exactly one Watcher manifest")
	}
	return watcher.DecodeManifest(&envelope)
}

type mutationResult struct {
	cmdio.SingleMutation

	Outcome watcher.MutationOutcome `json:"outcome" yaml:"outcome"`
}

func newMutationResult(action string, saved watcher.Watcher) mutationResult {
	envelope := cmdio.NewSingleMutation(action, cmdio.MutationTarget{Kind: watcher.WatcherKind, Name: saved.GetResourceName(), ID: saved.ServerID()})
	changed := saved.MutationOutcome().Changed
	envelope.Changed = &changed
	return mutationResult{SingleMutation: envelope, Outcome: saved.MutationOutcome()}
}

func emitMutation(cmd *cobra.Command, opts *cmdio.Options, verb string, saved watcher.Watcher, partial *watcher.PartialCompletionError) error {
	operation := verb + "d"
	if !saved.MutationOutcome().Changed {
		operation = "unchanged"
	}
	result := newMutationResult(operation, saved)
	if partial != nil {
		result.Error = partial.Error() + "; inspect with gcx assistant watchers get " + saved.ServerID() + " and reapply the desired manifest with update after resolving the failed stage"
	}
	if err := opts.Encode(cmd.OutOrStdout(), result); err != nil {
		return err
	}
	if partial != nil {
		return gcxerrors.NewEmittedError(gcxerrors.ExitPartialFailure, partial)
	}
	return nil
}

type deleteOpts struct {
	IO    cmdio.Options
	Ref   string
	Force bool
}

func (o *deleteOpts) setup(flags *pflag.FlagSet) {
	bindIO(&o.IO, flags)
	flags.BoolVar(&o.Force, "force", false, "Delete without confirmation")
}
func (o *deleteOpts) Validate() error {
	if strings.TrimSpace(o.Ref) == "" {
		return errors.New("WATCHER must be a nonempty resource name or server ID; try gcx assistant watchers delete checkout-health --force")
	}
	return o.IO.Validate()
}
func newDeleteCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &deleteOpts{}
	cmd := &cobra.Command{Use: "delete WATCHER", Short: "Delete an Assistant Watcher.", Long: "Delete one Watcher by resource name or server ID. Ambiguous names report candidate IDs. Confirmation is required unless --force or the shared auto-approval setting applies; use --force in agent mode. Deletion does not require exporting configuration or reading automatic-recalibration enrollment. Use gcx resources delete --dry-run for identity selection without deletion.", Args: cobra.ExactArgs(1), Example: "  gcx assistant watchers delete checkout-health\n  gcx assistant watchers delete example-id --force\n  gcx assistant watchers delete example-id --force -o json", RunE: func(cmd *cobra.Command, args []string) error {
		opts.Ref = args[0]
		if err := opts.Validate(); err != nil {
			return err
		}
		proceed, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.ErrOrStderr(), opts.Force, fmt.Sprintf("Delete Watcher %q?", opts.Ref))
		if err != nil {
			return err
		}
		if !proceed {
			code := gcxerrors.ExitCancelled
			return &gcxerrors.DetailedError{Summary: "delete cancelled", Details: fmt.Sprintf("Watcher %q was not deleted.", opts.Ref), ExitCode: &code}
		}
		client, namespace, err := newClient(cmd, loader)
		if err != nil {
			return err
		}
		selected, err := watcher.ResolveDeleteIdentity(cmd.Context(), client, opts.Ref)
		if err != nil {
			return err
		}
		crud := watcher.NewTypedCRUDForClient(client, namespace)
		if err := crud.Delete(cmd.Context(), selected.ID); err != nil {
			return err
		}
		result := mutationResult{SingleMutation: cmdio.NewSingleMutation("deleted", cmdio.MutationTarget{Kind: watcher.WatcherKind, Name: selected.Name, ID: selected.ID}), Outcome: watcher.MutationOutcome{Changed: true, Configuration: "deleted", Enrollment: "not-required", Readback: "not-required"}}
		result.Changed = new(true)
		return opts.IO.Encode(cmd.OutOrStdout(), result)
	}}
	markExperimental(cmd)
	cmd.Annotations[agent.AnnotationTokenCost] = "small"
	opts.setup(cmd.Flags())
	return cmd
}
