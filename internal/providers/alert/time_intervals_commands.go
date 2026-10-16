package alert

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/native"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const timeIntervalKind = "TimeInterval"

func timeIntervalsCommands(loader GrafanaConfigLoader) *cobra.Command {
	return newTimeIntervalsCommand(native.Bind(loader, native.Config{Group: routingTreesGroup, Resource: "timeintervals"}))
}

func newTimeIntervalsCommand(binding native.Binding) *cobra.Command {
	cmd := &cobra.Command{Use: "time-intervals", Short: "Manage notification time intervals by title.", Long: `Manage time intervals through Grafana's native notifications API. Native
JSON/YAML manifests also work with generic resources commands. Titles are matched
exactly and case-sensitively across the complete inventory; API names are opaque.
For provisioning YAML/JSON/HCL, use gcx alert mute-timings export. Select one
exact title with --name <title> and choose yaml, json, or hcl with --format.
Provisioning exports include only Grafana-managed intervals; imported intervals
shown by list/get are not exported. Collection exports may be empty or partial,
and exporting an imported title returns not found. Exported bytes are not native
TimeInterval manifests.`}
	cmd.AddCommand(newTimeIntervalsListCommand(binding), newTimeIntervalsGetCommand(binding), newTimeIntervalsCreateCommand(binding), newTimeIntervalsUpdateCommand(binding), newTimeIntervalsDeleteCommand(binding))
	return cmd
}

type timeIntervalsReadOpts struct {
	IO         cmdio.Options
	APIVersion string
}

func (o *timeIntervalsReadOpts) setup(flags *pflag.FlagSet, list bool) {
	o.IO.RegisterCustomCodec("table", &timeIntervalTableCodec{})
	o.IO.RegisterCustomCodec("wide", &timeIntervalTableCodec{wide: true})
	if list {
		o.IO.DefaultFormat("table")
	} else {
		o.IO.DefaultFormat("yaml")
	}
	o.IO.BindFlags(flags)
	flags.StringVar(&o.APIVersion, "api-version", "", apiVersionFlagUsage)
}
func (o *timeIntervalsReadOpts) Validate(cmd *cobra.Command) error {
	if err := validateNativeAlertVersionFlag(cmd, o.APIVersion); err != nil {
		return err
	}
	return o.IO.Validate()
}
func newTimeIntervalsListCommand(binding native.Binding) *cobra.Command {
	opts := &timeIntervalsReadOpts{}
	cmd := &cobra.Command{Use: "list", Short: "List all time intervals with their titles and API names.", Aliases: []string{"ls"}, Args: cobra.NoArgs, Example: `  gcx alert time-intervals list
  gcx alert time-intervals list -o json
  gcx alert time-intervals list --api-version v0alpha1`, RunE: func(cmd *cobra.Command, _ []string) error {
		if err := opts.Validate(cmd); err != nil {
			return err
		}
		access, err := binding.Load(cmd.Context(), native.LoadOptions{APIVersion: opts.APIVersion})
		if err != nil {
			return err
		}
		list, err := access.Client.List(cmd.Context(), access.Descriptor, metav1.ListOptions{})
		if err != nil {
			return err
		}
		if list.Items == nil {
			list.Items = []unstructured.Unstructured{}
		}
		return opts.IO.Encode(cmd.OutOrStdout(), list)
	}}
	opts.setup(cmd.Flags(), true)
	return cmd
}
func newTimeIntervalsGetCommand(binding native.Binding) *cobra.Command {
	opts := &timeIntervalsReadOpts{}
	cmd := &cobra.Command{Use: "get <title>", Short: "Get a native time interval manifest by exact title.", Long: `Resolve an exact, case-sensitive spec.name across the complete inventory,
then fetch the native manifest using its server API name. Metadata and unknown
fields are preserved. The title argument is never treated as an encoded name.`, Args: cobra.ExactArgs(1), Example: `  gcx alert time-intervals get weekends
  gcx alert time-intervals get weekends -o json
  gcx alert time-intervals get weekends -o yaml > interval.yaml`, RunE: func(cmd *cobra.Command, args []string) error {
		if err := opts.Validate(cmd); err != nil {
			return err
		}
		if err := validateNativeAlertTitle(cmd, args[0]); err != nil {
			return err
		}
		access, err := binding.Load(cmd.Context(), native.LoadOptions{APIVersion: opts.APIVersion})
		if err != nil {
			return err
		}
		match, err := resolveNativeAlertTitle(cmd.Context(), access, args[0], "")
		if err != nil {
			return err
		}
		obj, err := access.Client.Get(cmd.Context(), access.Descriptor, match.GetName(), metav1.GetOptions{})
		if err != nil {
			return err
		}
		return opts.IO.Encode(cmd.OutOrStdout(), obj)
	}}
	opts.setup(cmd.Flags(), false)
	return cmd
}

type timeIntervalsWriteOpts struct {
	IO                   cmdio.Options
	Filename, APIVersion string
}

func (o *timeIntervalsWriteOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("text", &nativeAlertMutationTextCodec{})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
	flags.StringVarP(&o.Filename, "filename", "f", "", "Path to one native JSON/YAML TimeInterval manifest ('-' reads stdin)")
	flags.StringVar(&o.APIVersion, "api-version", "", "Must match the manifest's apiVersion when set; the manifest decides the version")
}
func (o *timeIntervalsWriteOpts) Validate(cmd *cobra.Command) error {
	if strings.TrimSpace(o.Filename) == "" {
		return fail.NewCommandUsageError(cmd, "--filename / -f is required and must not be empty", nil)
	}
	if err := validateNativeAlertVersionFlag(cmd, o.APIVersion); err != nil {
		return err
	}
	return o.IO.Validate()
}
func newTimeIntervalsCreateCommand(binding native.Binding) *cobra.Command {
	opts := &timeIntervalsWriteOpts{}
	cmd := &cobra.Command{Use: "create -f <file>", Short: "Create a new time interval from a native manifest.", Long: `Create a TimeInterval using the manifest's API version. Grafana derives the
API name from spec.name; source identity and concurrency metadata are removed
from the request. Existing titles fail with a conflict and are never updated.
The input file is unchanged.`, Args: cobra.NoArgs, Example: `  gcx alert time-intervals create -f interval.yaml
  gcx alert time-intervals create -f interval.json -o json
  cat interval.yaml | gcx alert time-intervals create -f -`, RunE: func(cmd *cobra.Command, _ []string) error {
		if err := opts.Validate(cmd); err != nil {
			return err
		}
		obj, err := native.ReadManifest(opts.Filename, cmd.InOrStdin())
		if err != nil {
			return err
		}
		version, err := nativeAlertManifestVersion(cmd, obj, opts.APIVersion, timeIntervalKind)
		if err != nil {
			return err
		}
		prepared := resources.PrepareNativeCreate(obj)
		access, err := binding.Load(cmd.Context(), native.LoadOptions{APIVersion: version})
		if err != nil {
			return err
		}
		created, err := access.Client.Create(cmd.Context(), access.Descriptor, prepared, metav1.CreateOptions{})
		if err != nil {
			return err
		}
		title, _, _ := unstructured.NestedString(created.Object, "spec", "name")
		return opts.IO.Encode(cmd.OutOrStdout(), nativeAlertMutation("created", timeIntervalKind, title, "", created))
	}}
	opts.setup(cmd.Flags())
	_ = cmd.MarkFlagRequired("filename")
	return cmd
}
func newTimeIntervalsUpdateCommand(binding native.Binding) *cobra.Command {
	opts := &timeIntervalsWriteOpts{}
	cmd := &cobra.Command{Use: "update <current-title> -f <file>", Short: "Replace an existing time interval using its fetched version.", Long: `The argument selects the current title; the manifest supplies the desired title.
A different spec.name explicitly renames the selected interval. The manifest must
carry a fetched metadata.resourceVersion and, when present, the selected object's
API name. Stale versions fail with conflict; missing titles are never created.
Fetch the new title after rename to refresh local metadata; files are unchanged.`, Args: cobra.ExactArgs(1), Example: `  gcx alert time-intervals get weekends -o yaml > interval.yaml
  gcx alert time-intervals update weekends -f interval.yaml
  gcx alert time-intervals get weekends -o yaml | gcx alert time-intervals update weekends -f -`, RunE: func(cmd *cobra.Command, args []string) error {
		if err := opts.Validate(cmd); err != nil {
			return err
		}
		if err := validateNativeAlertTitle(cmd, args[0]); err != nil {
			return err
		}
		obj, err := native.ReadManifest(opts.Filename, cmd.InOrStdin())
		if err != nil {
			return err
		}
		version, err := nativeAlertManifestVersion(cmd, obj, opts.APIVersion, timeIntervalKind)
		if err != nil {
			return err
		}
		if strings.TrimSpace(obj.GetResourceVersion()) == "" {
			return fail.NewCommandUsageError(cmd, "the manifest has no metadata.resourceVersion; fetch the current interval with 'gcx alert time-intervals get <title> -o yaml' before updating", nil)
		}
		access, err := binding.Load(cmd.Context(), native.LoadOptions{APIVersion: version})
		if err != nil {
			return err
		}
		current, err := resolveNativeAlertTitle(cmd.Context(), access, args[0], "")
		if err != nil {
			return err
		}
		if resources.IsReadOnlyNative(current) {
			return resources.ReadOnlyNativeError(current)
		}
		if obj.GetName() != "" && obj.GetName() != current.GetName() {
			return fail.NewCommandUsageError(cmd, fmt.Sprintf("manifest metadata.name %q does not match current title %q (API name %q)", obj.GetName(), args[0], current.GetName()), nil)
		}
		desired := obj.DeepCopy()
		desired.SetName(current.GetName())
		updated, err := access.Client.Update(cmd.Context(), access.Descriptor, desired, metav1.UpdateOptions{})
		if err != nil {
			if apierrors.IsConflict(err) {
				return fmt.Errorf("%w; re-fetch 'gcx alert time-intervals get %s -o yaml', re-apply edits, and update again", err, args[0])
			}
			return err
		}
		title, _, _ := unstructured.NestedString(updated.Object, "spec", "name")
		return opts.IO.Encode(cmd.OutOrStdout(), nativeAlertMutation("updated", timeIntervalKind, title, args[0], updated))
	}}
	opts.setup(cmd.Flags())
	_ = cmd.MarkFlagRequired("filename")
	return cmd
}

type timeIntervalsDeleteOpts struct {
	IO         cmdio.Options
	APIVersion string
	Force      bool
}

func (o *timeIntervalsDeleteOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("text", &nativeAlertMutationTextCodec{})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.APIVersion, "api-version", "", apiVersionFlagUsage)
	flags.BoolVar(&o.Force, "force", false, "Skip confirmation prompt")
}
func (o *timeIntervalsDeleteOpts) Validate(cmd *cobra.Command) error {
	if err := validateNativeAlertVersionFlag(cmd, o.APIVersion); err != nil {
		return err
	}
	return o.IO.Validate()
}
func newTimeIntervalsDeleteCommand(binding native.Binding) *cobra.Command {
	opts := &timeIntervalsDeleteOpts{}
	cmd := &cobra.Command{Use: "delete <title>", Short: "Delete a time interval by exact title after confirmation.", Aliases: []string{"rm"}, Args: cobra.ExactArgs(1), Example: `  gcx alert time-intervals delete weekends
  gcx alert time-intervals delete weekends --force
  gcx alert time-intervals delete weekends --force -o json`, RunE: func(cmd *cobra.Command, args []string) error {
		if err := opts.Validate(cmd); err != nil {
			return err
		}
		if err := validateNativeAlertTitle(cmd, args[0]); err != nil {
			return err
		}
		proceed, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.ErrOrStderr(), opts.Force, fmt.Sprintf("Delete time interval %q?", args[0]))
		if err != nil {
			return err
		}
		if !proceed {
			return cancelledNativeAlertDelete(timeIntervalKind, args[0])
		}
		access, err := binding.Load(cmd.Context(), native.LoadOptions{APIVersion: opts.APIVersion})
		if err != nil {
			return err
		}
		current, err := resolveNativeAlertTitle(cmd.Context(), access, args[0], "")
		if err != nil {
			return err
		}
		if resources.IsReadOnlyNative(current) {
			return resources.ReadOnlyNativeError(current)
		}
		if err := access.Client.Delete(cmd.Context(), access.Descriptor, current.GetName(), metav1.DeleteOptions{}); err != nil {
			return err
		}
		return opts.IO.Encode(cmd.OutOrStdout(), nativeAlertMutation("deleted", timeIntervalKind, args[0], "", current))
	}}
	opts.setup(cmd.Flags())
	return cmd
}

// Shared alert-local validation and identity resolution for the two native groups.
func validateNativeAlertVersionFlag(cmd *cobra.Command, version string) error {
	if cmd.Flags().Changed("api-version") && version == "" {
		return fail.NewCommandUsageError(cmd, "--api-version must not be empty", nil)
	}
	return validateAPIVersionFlag(cmd, version)
}
func validateNativeAlertTitle(cmd *cobra.Command, title string) error {
	if strings.TrimSpace(title) == "" {
		return fail.NewCommandUsageError(cmd, "title must be a nonempty string", nil)
	}
	return nil
}
func nativeAlertManifestVersion(cmd *cobra.Command, obj *unstructured.Unstructured, flagVersion, kind string) (string, error) {
	for _, field := range []string{"name", "resourceVersion"} {
		if _, _, err := unstructured.NestedString(obj.Object, "metadata", field); err != nil {
			return "", fail.NewCommandUsageError(cmd, "metadata."+field+" must be a string", nil)
		}
	}
	gv, err := schema.ParseGroupVersion(obj.GetAPIVersion())
	if err != nil || gv.Group != routingTreesGroup || gv.Version == "" {
		return "", fail.NewCommandUsageError(cmd, fmt.Sprintf("manifest apiVersion %q must be %s/<version>", obj.GetAPIVersion(), routingTreesGroup), nil)
	}
	if _, err := native.ParseAPIVersion(routingTreesGroup, obj.GetAPIVersion()); err != nil {
		return "", fail.NewCommandUsageError(cmd, err.Error(), nil)
	}
	if obj.GetKind() != kind {
		return "", fail.NewCommandUsageError(cmd, fmt.Sprintf("manifest kind %q must be %s", obj.GetKind(), kind), nil)
	}
	if flagVersion != "" {
		v, _ := native.ParseAPIVersion(routingTreesGroup, flagVersion)
		if v != gv.Version {
			return "", fail.NewCommandUsageError(cmd, fmt.Sprintf("--api-version %q conflicts with manifest apiVersion %q; the manifest decides the version", flagVersion, obj.GetAPIVersion()), nil)
		}
	}
	if resources.IsReadOnlyNative(obj) {
		return "", resources.ReadOnlyNativeError(obj)
	}
	if _, _, err := resources.NativeIdentity(obj); err != nil {
		return "", fail.NewCommandUsageError(cmd, err.Error(), nil)
	}
	return gv.Version, nil
}
func resolveNativeAlertTitle(ctx context.Context, access native.Access, title, kindFilter string) (*unstructured.Unstructured, error) {
	list, err := access.Client.List(ctx, access.Descriptor, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	field := "title"
	if access.Descriptor.Kind == timeIntervalKind {
		field = "name"
	}
	var matches []unstructured.Unstructured
	var candidates []string
	for _, item := range list.Items {
		t, _, _ := unstructured.NestedString(item.Object, "spec", field)
		if t != title {
			continue
		}
		candidate := item.GetName()
		if access.Descriptor.Kind == "TemplateGroup" {
			k, err := normalizedTemplateKind(&item)
			if err != nil {
				if kindFilter != "" {
					continue
				}
				k, _, _ = unstructured.NestedString(item.Object, "spec", "kind")
			}
			if kindFilter != "" && k != kindFilter {
				continue
			}
			candidate = k + "/" + candidate
		}
		matches = append(matches, item)
		candidates = append(candidates, candidate)
	}
	if len(matches) == 0 {
		return nil, apierrors.NewNotFound(schema.GroupResource{Group: access.Descriptor.GroupVersion.Group, Resource: access.Descriptor.Plural}, title)
	}
	if len(matches) > 1 {
		guidance := ""
		if access.Descriptor.Kind == "TemplateGroup" {
			guidance = "; use --kind grafana or --kind mimir to disambiguate"
		}
		return nil, fmt.Errorf("title %q is ambiguous: matching API names %s%s", title, strings.Join(candidates, ", "), guidance)
	}
	return &matches[0], nil
}

type nativeAlertMutationResult struct {
	cmdio.SingleMutation `json:",inline" yaml:",inline"`

	Title           string `json:"title" yaml:"title"`
	PreviousTitle   string `json:"previousTitle,omitempty" yaml:"previousTitle,omitempty"`
	ResourceVersion string `json:"resourceVersion,omitempty" yaml:"resourceVersion,omitempty"`
}

func nativeAlertMutation(action, kind, title, oldTitle string, obj *unstructured.Unstructured) nativeAlertMutationResult {
	target := cmdio.MutationTarget{Kind: kind, Name: obj.GetName(), Namespace: obj.GetNamespace(), UID: string(obj.GetUID())}
	result := nativeAlertMutationResult{SingleMutation: cmdio.NewSingleMutation(action, target), Title: title, ResourceVersion: obj.GetResourceVersion()}
	if oldTitle != "" && oldTitle != title {
		result.PreviousTitle = oldTitle
	}
	if action == "created" || action == "deleted" {
		changed := true
		result.Changed = &changed
	}
	return result
}
func nativeAlertMutationLine(m nativeAlertMutationResult) string {
	label := "time interval"
	if m.Target.Kind == "TemplateGroup" {
		label = "template group"
	}
	if m.PreviousTitle != "" {
		return fmt.Sprintf("%s %q renamed to %q (API name %q)", label, m.PreviousTitle, m.Title, m.Target.Name)
	}
	return fmt.Sprintf("%s %q %s (API name %q)", label, m.Title, m.Action, m.Target.Name)
}

type nativeAlertMutationTextCodec struct{}

func (*nativeAlertMutationTextCodec) Format() format.Format { return "text" }
func (*nativeAlertMutationTextCodec) Decode(io.Reader, any) error {
	return errors.New("text format does not support decoding")
}
func (*nativeAlertMutationTextCodec) Encode(w io.Writer, v any) error {
	m, ok := v.(nativeAlertMutationResult)
	if !ok {
		return fmt.Errorf("native alert mutation: unsupported type %T", v)
	}
	cmdio.Success(w, "%s", nativeAlertMutationLine(m))
	return nil
}
func cancelledNativeAlertDelete(kind, title string) error {
	return &gcxerrors.DetailedError{Summary: "delete cancelled", Details: fmt.Sprintf("confirmation was declined; %s %q was not changed", kind, title), ExitCode: new(gcxerrors.ExitCancelled)}
}

type timeIntervalTableCodec struct{ wide bool }

func (c *timeIntervalTableCodec) Format() format.Format {
	if c.wide {
		return "wide"
	}
	return "table"
}
func (*timeIntervalTableCodec) Decode(io.Reader, any) error {
	return errors.New("table format does not support decoding")
}
func (c *timeIntervalTableCodec) Encode(w io.Writer, v any) error {
	var items []unstructured.Unstructured
	switch val := v.(type) {
	case *unstructured.UnstructuredList:
		items = val.Items
	case *unstructured.Unstructured:
		items = []unstructured.Unstructured{*val}
	default:
		return fmt.Errorf("time interval table: unsupported type %T", v)
	}
	headers := []string{"TITLE", "API_NAME", "INTERVALS", "READ-ONLY"}
	if c.wide {
		headers = append(headers, "PROVENANCE")
	}
	table := style.NewTable(headers...)
	for _, item := range items {
		title, _, _ := unstructured.NestedString(item.Object, "spec", "name")
		intervals, _, _ := unstructured.NestedSlice(item.Object, "spec", "time_intervals")
		row := []string{title, item.GetName(), strconv.Itoa(len(intervals)), strconv.FormatBool(resources.IsReadOnlyNative(&item))}
		if c.wide {
			row = append(row, item.GetAnnotations()[provenanceAnnotation])
		}
		table.Row(row...)
	}
	return table.Render(w)
}
