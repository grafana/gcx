package scim

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/native"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/validation"
)

type settingsUpdateOptions struct {
	IO                                                       cmdio.Options
	UserSync, GroupSync, RejectNonProvisioned, DryRun, Force bool
}

func (o *settingsUpdateOptions) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("text", &configMutationTextCodec{})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
	flags.BoolVar(&o.UserSync, "user-sync", false, "Set SCIM user sync; true enables, false disables; omit to keep unchanged")
	flags.BoolVar(&o.GroupSync, "group-sync", false, "Set SCIM group sync; true enables, false disables; omit to keep unchanged")
	flags.BoolVar(&o.RejectNonProvisioned, "reject-non-provisioned", false, "Set rejection of users that SCIM did not provision; true enables, false disables; omit to keep unchanged")
	flags.BoolVar(&o.Force, "force", false, "Skip confirmation when enabling rejection of users that SCIM did not provision")
	flags.BoolVar(&o.DryRun, "dry-run", false, "Show current and proposed settings without a write")
}
func (o *settingsUpdateOptions) Validate(cmd *cobra.Command) error {
	if !cmd.Flags().Changed("user-sync") && !cmd.Flags().Changed("group-sync") && !cmd.Flags().Changed("reject-non-provisioned") {
		return fail.NewCommandUsageError(cmd, "set at least one SCIM setting flag", nil)
	}
	return o.IO.Validate()
}

func (o *settingsUpdateOptions) confirmLockout(cmd *cobra.Command) error {
	if o.DryRun || !cmd.Flags().Changed("reject-non-provisioned") || !o.RejectNonProvisioned {
		return nil
	}
	proceed, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.ErrOrStderr(), o.Force, "Block sign-in for users that SCIM did not provision?")
	if err != nil {
		return err
	}
	if !proceed {
		exitCode := gcxerrors.ExitCancelled
		return &gcxerrors.DetailedError{Summary: "settings update cancelled", ExitCode: &exitCode}
	}
	return nil
}

func newSettingsCommand(binding native.Binding) *cobra.Command {
	cmd := &cobra.Command{Use: "settings", Short: "Manage SCIM sync settings"}
	cmd.AddCommand(newSettingsGetCommand(binding), newSettingsUpdateCommand(binding))
	return cmd
}
func newSettingsGetCommand(binding native.Binding) *cobra.Command {
	o := &cmdio.Options{}
	cmd := &cobra.Command{Use: "get", Short: "Get SCIM sync settings", Args: cobra.NoArgs, Example: "  gcx scim settings get -o json", RunE: func(cmd *cobra.Command, _ []string) error {
		if err := o.Validate(); err != nil {
			return err
		}
		access, err := binding.Load(cmd.Context(), native.LoadOptions{})
		if err != nil {
			return err
		}
		item, err := access.Client.Get(cmd.Context(), access.Descriptor, "default", metav1.GetOptions{})
		if err != nil {
			return err
		}
		safe, err := safeConfig(item)
		if err != nil {
			return err
		}
		return o.Encode(cmd.OutOrStdout(), safe)
	}}
	o.DefaultFormat("yaml")
	o.BindFlags(cmd.Flags())
	return cmd
}
func newSettingsUpdateCommand(binding native.Binding) *cobra.Command {
	o := &settingsUpdateOptions{}
	cmd := &cobra.Command{Use: "update", Short: "Update selected SCIM sync settings", Long: "Update only the settings named by flags. Keep other settings unchanged.\nThe update uses the current resource version. A conflict requires a new attempt.\nUse --dry-run to show current and proposed settings without a write.\nEnabling --reject-non-provisioned can block sign-in and requires confirmation; use --force to skip it.", Args: cobra.NoArgs, Example: "  gcx scim settings update --user-sync=true --dry-run", RunE: func(cmd *cobra.Command, _ []string) error {
		if err := o.Validate(cmd); err != nil {
			return err
		}
		if err := o.confirmLockout(cmd); err != nil {
			return err
		}
		access, err := binding.Load(cmd.Context(), native.LoadOptions{})
		if err != nil {
			return err
		}
		item, err := access.Client.Get(cmd.Context(), access.Descriptor, "default", metav1.GetOptions{})
		if err != nil {
			return err
		}
		if item.GetResourceVersion() == "" {
			return errors.New("SCIM config has no resource version; the update is unsafe")
		}
		proposed := item.DeepCopy()
		changes := []struct {
			flag, field string
			value       bool
		}{
			{"user-sync", "enableUserSync", o.UserSync},
			{"group-sync", "enableGroupSync", o.GroupSync},
			{"reject-non-provisioned", "rejectNonProvisionedUsers", o.RejectNonProvisioned},
		}
		for _, change := range changes {
			if cmd.Flags().Changed(change.flag) {
				if err := unstructured.SetNestedField(proposed.Object, change.value, "spec", change.field); err != nil {
					return err
				}
			}
		}
		currentView, err := safeConfig(item)
		if err != nil {
			return err
		}
		proposedView, err := safeConfig(proposed)
		if err != nil {
			return err
		}
		result := configMutationResult{
			SingleMutation: cmdio.NewSingleMutation("updated", cmdio.MutationTarget{Kind: "SCIMConfig", Name: proposed.GetName(), Namespace: proposed.GetNamespace()}),
			Current:        currentView.Settings,
			Proposed:       proposedView.Settings,
		}
		if o.DryRun {
			result.DryRun = true
			return o.IO.Encode(cmd.OutOrStdout(), result)
		}
		updated, err := access.Client.Update(cmd.Context(), access.Descriptor, proposed, metav1.UpdateOptions{})
		if apierrors.IsConflict(err) {
			return fmt.Errorf("%w; SCIM config changed; run 'gcx scim settings get' and apply the update again", err)
		}
		if err != nil {
			return err
		}
		result.Target = cmdio.MutationTarget{Kind: "SCIMConfig", Name: updated.GetName(), Namespace: updated.GetNamespace()}
		return o.IO.Encode(cmd.OutOrStdout(), result)
	}}
	o.setup(cmd.Flags())
	return cmd
}

// configSettingsView is a partial view of public settings, not a resource manifest.
type configSettingsView struct {
	Name            string          `json:"name" yaml:"name"`
	Namespace       string          `json:"namespace" yaml:"namespace"`
	ResourceVersion string          `json:"resourceVersion" yaml:"resourceVersion"`
	Settings        map[string]bool `json:"settings" yaml:"settings"`
}

// safeConfig selects public settings. Unknown fields can contain credentials.
func safeConfig(item *unstructured.Unstructured) (configSettingsView, error) {
	safe := configSettingsView{Name: item.GetName(), Namespace: item.GetNamespace(), ResourceVersion: item.GetResourceVersion(), Settings: map[string]bool{}}
	for _, key := range []string{"enableUserSync", "enableGroupSync", "rejectNonProvisionedUsers"} {
		value, found, err := unstructured.NestedBool(item.Object, "spec", key)
		if err != nil {
			return configSettingsView{}, fmt.Errorf("invalid SCIM setting %s: expected a boolean", key)
		}
		if found {
			safe.Settings[key] = value
		}
	}
	return safe, nil
}

type configMutationResult struct {
	cmdio.SingleMutation `yaml:",inline"`

	Current  map[string]bool `json:"current" yaml:"current"`
	Proposed map[string]bool `json:"proposed" yaml:"proposed"`
}

type configMutationTextCodec struct{}

func (*configMutationTextCodec) Format() format.Format { return "text" }
func (*configMutationTextCodec) Encode(w io.Writer, value any) error {
	result, ok := value.(configMutationResult)
	if !ok {
		return errors.New("SCIM config summary requires a config mutation result")
	}
	heading := "Updated SCIM sync settings"
	if result.DryRun {
		heading = "Would update SCIM sync settings (dry run)"
	}
	var text strings.Builder
	fmt.Fprintln(&text, heading)
	changed := false
	for _, key := range []string{"enableUserSync", "enableGroupSync", "rejectNonProvisionedUsers"} {
		current, hadCurrent := result.Current[key]
		proposed, hasProposed := result.Proposed[key]
		if hadCurrent == hasProposed && current == proposed {
			continue
		}
		before, after := "unset", "unset"
		if hadCurrent {
			before = strconv.FormatBool(current)
		}
		if hasProposed {
			after = strconv.FormatBool(proposed)
		}
		fmt.Fprintf(&text, "  %s: %s -> %s\n", key, before, after)
		changed = true
	}
	if !changed {
		fmt.Fprintln(&text, "  No setting values change")
	}
	_, err := io.WriteString(w, text.String())
	return err
}
func (*configMutationTextCodec) Decode(io.Reader, any) error {
	return errors.New("text codec does not support decoding")
}

type endpointResult struct {
	URL string `json:"url" yaml:"url"`
}

func newEndpointCommand(binding native.Binding) *cobra.Command {
	parent := &cobra.Command{Use: "endpoint", Short: "Show the SCIM tenant endpoint"}
	o := &cmdio.Options{}
	cmd := &cobra.Command{Use: "get", Short: "Get the SCIM tenant base URL", Long: "Get the SCIM tenant base URL from the active Grafana server and the namespace returned by the SCIM config API. No token is included.", Args: cobra.NoArgs, Example: "  gcx scim endpoint get -o json", RunE: func(cmd *cobra.Command, _ []string) error {
		if err := o.Validate(); err != nil {
			return err
		}
		access, err := binding.Load(cmd.Context(), native.LoadOptions{})
		if err != nil {
			return err
		}
		item, err := access.Client.Get(cmd.Context(), access.Descriptor, "default", metav1.GetOptions{})
		if err != nil {
			return err
		}
		endpoint, err := tenantEndpoint(access.Config.GrafanaURL, item.GetNamespace(), access.Descriptor.GroupVersion.Version)
		if err != nil {
			return err
		}
		return o.Encode(cmd.OutOrStdout(), endpointResult{URL: endpoint})
	}}
	o.DefaultFormat("yaml")
	o.BindFlags(cmd.Flags())
	parent.AddCommand(cmd)
	return parent
}
func tenantEndpoint(server, namespace, version string) (string, error) {
	u, err := url.Parse(server)
	if err != nil || u == nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("SCIM endpoint requires a trusted Grafana server URL without credentials, query, or fragment")
	}
	if len(validation.IsDNS1123Label(namespace)) != 0 {
		return "", errors.New("SCIM config returned an invalid or empty tenant namespace")
	}
	if len(validation.IsDNS1035Label(version)) != 0 {
		return "", errors.New("SCIM discovery returned an invalid API version")
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/apis/" + scimGroup + "/" + version + "/namespaces/" + namespace
	u.RawPath = ""
	return u.String(), nil
}
