package alert

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/format"
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
)

const templateGroupKind = "TemplateGroup"

func templateGroupsCommands(loader GrafanaConfigLoader) *cobra.Command {
	return newTemplateGroupsCommand(native.Bind(loader, native.Config{Group: routingTreesGroup, Resource: "templategroups"}))
}

func newTemplateGroupsCommand(binding native.Binding) *cobra.Command {
	cmd := &cobra.Command{Use: "template-groups", Short: "Manage native notification template groups by title.", Long: `Manage TemplateGroup manifests through Grafana's native notifications API.
List includes Grafana-managed groups and read-only imported Mimir groups.
Use templates for the existing provisioning-payload commands.`}
	cmd.AddCommand(newTemplateGroupsListCommand(binding), newTemplateGroupsGetCommand(binding), newTemplateGroupsCreateCommand(binding), newTemplateGroupsUpdateCommand(binding), newTemplateGroupsDeleteCommand(binding))
	return cmd
}

type templateGroupsListOpts struct {
	IO         cmdio.Options
	APIVersion string
}

func (o *templateGroupsListOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &templateGroupTableCodec{})
	o.IO.RegisterCustomCodec("wide", &templateGroupTableCodec{wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.APIVersion, "api-version", "", apiVersionFlagUsage)
}
func (o *templateGroupsListOpts) Validate(cmd *cobra.Command) error {
	if err := validateNativeAlertVersionFlag(cmd, o.APIVersion); err != nil {
		return err
	}
	return o.IO.Validate()
}
func newTemplateGroupsListCommand(binding native.Binding) *cobra.Command {
	opts := &templateGroupsListOpts{}
	cmd := &cobra.Command{Use: "list", Short: "List all template groups, including read-only imports.", Aliases: []string{"ls"}, Args: cobra.NoArgs,
		Example: `  gcx alert template-groups list
  gcx alert template-groups list -o wide
  gcx alert template-groups list -o json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
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
	opts.setup(cmd.Flags())
	return cmd
}

type templateGroupsGetOpts struct {
	IO               cmdio.Options
	APIVersion, Kind string
}

func (o *templateGroupsGetOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &templateGroupTableCodec{})
	o.IO.RegisterCustomCodec("wide", &templateGroupTableCodec{wide: true})
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.APIVersion, "api-version", "", apiVersionFlagUsage)
	flags.StringVar(&o.Kind, "kind", "", "Select grafana or mimir when titles overlap; omitted requires a unique title")
}
func (o *templateGroupsGetOpts) Validate(cmd *cobra.Command) error {
	if err := validateNativeAlertVersionFlag(cmd, o.APIVersion); err != nil {
		return err
	}
	if err := validateTemplateKindFlag(cmd, o.Kind); err != nil {
		return err
	}
	return o.IO.Validate()
}
func newTemplateGroupsGetCommand(binding native.Binding) *cobra.Command {
	opts := &templateGroupsGetOpts{}
	cmd := &cobra.Command{Use: "get <title>", Short: "Get a native template group manifest by exact title.", Args: cobra.ExactArgs(1),
		Example: `  gcx alert template-groups get shared
  gcx alert template-groups get shared --kind mimir
  gcx alert template-groups get shared --kind grafana -o json`,
		RunE: func(cmd *cobra.Command, args []string) error {
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
			selected, err := resolveNativeAlertTitle(cmd.Context(), access, args[0], opts.Kind)
			if err != nil {
				return err
			}
			obj, err := access.Client.Get(cmd.Context(), access.Descriptor, selected.GetName(), metav1.GetOptions{})
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), obj)
		}}
	opts.setup(cmd.Flags())
	return cmd
}

type templateGroupsWriteOpts struct {
	IO                   cmdio.Options
	APIVersion, Filename string
}

func (o *templateGroupsWriteOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("text", &nativeAlertMutationTextCodec{})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
	flags.StringVarP(&o.Filename, "filename", "f", "", "Path to a native JSON/YAML TemplateGroup manifest ('-' reads stdin)")
	flags.StringVar(&o.APIVersion, "api-version", "", "Must match the manifest's apiVersion when set; the manifest decides the version")
}
func (o *templateGroupsWriteOpts) Validate(cmd *cobra.Command) error {
	if strings.TrimSpace(o.Filename) == "" {
		return fail.NewCommandUsageError(cmd, "--filename / -f is required", nil)
	}
	if err := validateNativeAlertVersionFlag(cmd, o.APIVersion); err != nil {
		return err
	}
	return o.IO.Validate()
}
func (o *templateGroupsWriteOpts) read(cmd *cobra.Command) (*unstructured.Unstructured, string, error) {
	obj, err := native.ReadManifest(o.Filename, cmd.InOrStdin())
	if err != nil {
		return nil, "", err
	}
	version, err := nativeAlertManifestVersion(cmd, obj, o.APIVersion, templateGroupKind)
	if err != nil {
		return nil, "", err
	}
	return obj, version, nil
}
func newTemplateGroupsCreateCommand(binding native.Binding) *cobra.Command {
	opts := &templateGroupsWriteOpts{}
	cmd := &cobra.Command{Use: "create -f <file>", Short: "Create a Grafana-managed template group from a native manifest.", Long: `Create one Grafana-managed template group. Create never overwrites an existing group.
Source server identity and concurrency metadata are omitted from the request.
Imported Mimir groups must be managed in the external Alertmanager configuration.`, Args: cobra.NoArgs,
		Example: `  gcx alert template-groups create -f group.yaml
  gcx alert template-groups create -f group.json -o json
  cat group.yaml | gcx alert template-groups create -f -`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(cmd); err != nil {
				return err
			}
			obj, version, err := opts.read(cmd)
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
			title, _, _ := unstructured.NestedString(created.Object, "spec", "title")
			return opts.IO.Encode(cmd.OutOrStdout(), nativeAlertMutation("created", templateGroupKind, title, "", created))
		}}
	opts.setup(cmd.Flags())
	_ = cmd.MarkFlagRequired("filename")
	return cmd
}
func newTemplateGroupsUpdateCommand(binding native.Binding) *cobra.Command {
	opts := &templateGroupsWriteOpts{}
	cmd := &cobra.Command{Use: "update <current-title> -f <file>", Short: "Update or rename the selected Grafana-managed template group.", Long: `The argument selects the current title; the manifest supplies the desired title.
The manifest must retain metadata.resourceVersion from get. Template kind cannot
change. Server conflicts are preserved; fetch the current manifest and reapply
edits to retry. After rename, fetch the new title or pull again to synchronize
local files.`, Args: cobra.ExactArgs(1),
		Example: `  gcx alert template-groups get shared --kind grafana > group.yaml
  gcx alert template-groups update shared -f group.yaml
  cat group.yaml | gcx alert template-groups update shared -f - -o json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(cmd); err != nil {
				return err
			}
			if err := validateNativeAlertTitle(cmd, args[0]); err != nil {
				return err
			}
			obj, version, err := opts.read(cmd)
			if err != nil {
				return err
			}
			if strings.TrimSpace(obj.GetResourceVersion()) == "" {
				return fail.NewCommandUsageError(cmd, "the manifest has no metadata.resourceVersion; fetch the current group with gcx alert template-groups get first", nil)
			}
			access, err := binding.Load(cmd.Context(), native.LoadOptions{APIVersion: version})
			if err != nil {
				return err
			}
			// read rejects mimir manifests, so the subject is always Grafana-managed.
			selected, err := resolveNativeAlertTitle(cmd.Context(), access, args[0], "grafana")
			if err != nil {
				return err
			}
			if resources.IsReadOnlyNative(selected) {
				return resources.ReadOnlyNativeError(selected)
			}
			if obj.GetName() != "" && obj.GetName() != selected.GetName() {
				return fail.NewCommandUsageError(cmd, fmt.Sprintf("manifest metadata.name %q must match the selected current group's API name %q; template kind cannot change", obj.GetName(), selected.GetName()), nil)
			}
			obj.SetName(selected.GetName())
			updated, err := access.Client.Update(cmd.Context(), access.Descriptor, obj, metav1.UpdateOptions{})
			if err != nil {
				if apierrors.IsConflict(err) {
					return fmt.Errorf("%w; fetch the current group with gcx alert template-groups get, reapply edits, and retry", err)
				}
				return err
			}
			title, _, _ := unstructured.NestedString(updated.Object, "spec", "title")
			return opts.IO.Encode(cmd.OutOrStdout(), nativeAlertMutation("updated", templateGroupKind, title, args[0], updated))
		}}
	opts.setup(cmd.Flags())
	_ = cmd.MarkFlagRequired("filename")
	return cmd
}

type templateGroupsDeleteOpts struct {
	IO               cmdio.Options
	APIVersion, Kind string
	Force            bool
}

func (o *templateGroupsDeleteOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("text", &nativeAlertMutationTextCodec{})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.APIVersion, "api-version", "", apiVersionFlagUsage)
	flags.StringVar(&o.Kind, "kind", "", "Select grafana or mimir when titles overlap; omitted requires a unique title")
	flags.BoolVar(&o.Force, "force", false, "Skip confirmation prompt; imported groups remain read-only")
}
func (o *templateGroupsDeleteOpts) Validate(cmd *cobra.Command) error {
	if err := validateNativeAlertVersionFlag(cmd, o.APIVersion); err != nil {
		return err
	}
	if err := validateTemplateKindFlag(cmd, o.Kind); err != nil {
		return err
	}
	return o.IO.Validate()
}
func newTemplateGroupsDeleteCommand(binding native.Binding) *cobra.Command {
	opts := &templateGroupsDeleteOpts{}
	cmd := &cobra.Command{Use: "delete <title>", Short: "Delete one Grafana-managed template group by exact title.", Aliases: []string{"rm"}, Args: cobra.ExactArgs(1),
		Example: `  gcx alert template-groups delete shared
  gcx alert template-groups delete shared --kind grafana --force
  gcx alert template-groups delete shared --force -o json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(cmd); err != nil {
				return err
			}
			if err := validateNativeAlertTitle(cmd, args[0]); err != nil {
				return err
			}
			proceed, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.ErrOrStderr(), opts.Force, fmt.Sprintf("Delete template group %q?", args[0]))
			if err != nil {
				return err
			}
			if !proceed {
				return cancelledNativeAlertDelete(templateGroupKind, args[0])
			}
			access, err := binding.Load(cmd.Context(), native.LoadOptions{APIVersion: opts.APIVersion})
			if err != nil {
				return err
			}
			obj, err := resolveNativeAlertTitle(cmd.Context(), access, args[0], opts.Kind)
			if err != nil {
				return err
			}
			if resources.IsReadOnlyNative(obj) {
				return resources.ReadOnlyNativeError(obj)
			}
			if err := access.Client.Delete(cmd.Context(), access.Descriptor, obj.GetName(), metav1.DeleteOptions{}); err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), nativeAlertMutation("deleted", templateGroupKind, args[0], "", obj))
		}}
	opts.setup(cmd.Flags())
	return cmd
}
func validateTemplateKindFlag(cmd *cobra.Command, kind string) error {
	if (cmd.Flags().Changed("kind") && kind == "") || (kind != "" && kind != "grafana" && kind != "mimir") {
		return fail.NewCommandUsageError(cmd, fmt.Sprintf("--kind %q must be grafana or mimir", kind), nil)
	}
	return nil
}
func normalizedTemplateKind(obj *unstructured.Unstructured) (string, error) {
	kind, found, err := unstructured.NestedString(obj.Object, "spec", "kind")
	if err != nil {
		return "", fmt.Errorf("spec.kind must be grafana or mimir: %w", err)
	}
	if !found {
		return "grafana", nil
	}
	if kind != "grafana" && kind != "mimir" {
		return "", fmt.Errorf("spec.kind %q must be grafana or mimir", kind)
	}
	return kind, nil
}

type templateGroupTableCodec struct{ wide bool }

func (c *templateGroupTableCodec) Format() format.Format {
	if c.wide {
		return "wide"
	}
	return "table"
}
func (c *templateGroupTableCodec) Decode(io.Reader, any) error {
	return errors.New("table format does not support decoding")
}
func (c *templateGroupTableCodec) Encode(w io.Writer, v any) error {
	var items []unstructured.Unstructured
	switch value := v.(type) {
	case *unstructured.UnstructuredList:
		items = value.Items
	case *unstructured.Unstructured:
		items = []unstructured.Unstructured{*value}
	default:
		return fmt.Errorf("template group table: unsupported type %T", v)
	}
	headers := []string{"TITLE", "API_NAME", "KIND", "READ-ONLY"}
	if c.wide {
		headers = append(headers, "PROVENANCE")
	}
	table := style.NewTable(headers...)
	for _, item := range items {
		title, _, _ := unstructured.NestedString(item.Object, "spec", "title")
		kind, err := normalizedTemplateKind(&item)
		readOnly := "false"
		if err != nil {
			kind, _, _ = unstructured.NestedString(item.Object, "spec", "kind")
			readOnly = "unknown"
		} else if resources.IsReadOnlyNative(&item) {
			readOnly = "true"
		}
		row := []string{title, item.GetName(), kind, readOnly}
		if c.wide {
			row = append(row, item.GetAnnotations()[provenanceAnnotation])
		}
		table.Row(row...)
	}
	return table.Render(w)
}
