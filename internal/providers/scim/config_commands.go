package scim

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/grafana/gcx/cmd/gcx/fail"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers/native"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/validation"
)

type configOptions struct {
	IO                                                cmdio.Options
	UserSync, GroupSync, RejectNonProvisioned, DryRun bool
}

func (o *configOptions) setup(flags *pflag.FlagSet, update bool) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
	if update {
		flags.BoolVar(&o.UserSync, "user-sync", false, "Enable SCIM user sync")
		flags.BoolVar(&o.GroupSync, "group-sync", false, "Enable SCIM group sync")
		flags.BoolVar(&o.RejectNonProvisioned, "reject-non-provisioned", false, "Reject users that SCIM did not provision")
		flags.BoolVar(&o.DryRun, "dry-run", false, "Show the proposed settings without a write")
	}
}
func (o *configOptions) Validate(cmd *cobra.Command, update bool) error {
	if update && !cmd.Flags().Changed("user-sync") && !cmd.Flags().Changed("group-sync") && !cmd.Flags().Changed("reject-non-provisioned") {
		return fail.NewCommandUsageError(cmd, "set at least one SCIM setting flag", nil)
	}
	return o.IO.Validate()
}

func newConfigCommand(binding native.Binding) *cobra.Command {
	cmd := &cobra.Command{Use: "config", Short: "Manage SCIM sync settings"}
	cmd.AddCommand(newConfigGetCommand(binding), newConfigUpdateCommand(binding))
	return cmd
}
func newConfigGetCommand(binding native.Binding) *cobra.Command {
	o := &configOptions{}
	cmd := &cobra.Command{Use: "get", Short: "Get SCIM sync settings", Args: cobra.NoArgs, Example: "  gcx scim config get -o json", RunE: func(cmd *cobra.Command, _ []string) error {
		if err := o.Validate(cmd, false); err != nil {
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
		return o.IO.Encode(cmd.OutOrStdout(), safe)
	}}
	o.setup(cmd.Flags(), false)
	return cmd
}
func newConfigUpdateCommand(binding native.Binding) *cobra.Command {
	o := &configOptions{}
	cmd := &cobra.Command{Use: "update", Short: "Update selected SCIM sync settings", Long: "Update only the settings named by flags. Keep other settings unchanged.\nThe update uses the current resource version. A conflict requires a new attempt.\nUse --dry-run to read and show the proposed settings without a write.", Args: cobra.NoArgs, Example: "  gcx scim config update --user-sync=true --dry-run", RunE: func(cmd *cobra.Command, _ []string) error {
		if err := o.Validate(cmd, true); err != nil {
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
		if o.DryRun {
			safe, err := safeConfig(proposed)
			if err != nil {
				return err
			}
			return o.IO.Encode(cmd.OutOrStdout(), map[string]any{"type": "gcx.scim_config_preview", "schema_version": "1", "dry_run": true, "config": safe})
		}
		updated, err := access.Client.Update(cmd.Context(), access.Descriptor, proposed, metav1.UpdateOptions{})
		if apierrors.IsConflict(err) {
			return fmt.Errorf("%w; SCIM config changed; run 'gcx scim config get' and apply the update again", err)
		}
		if err != nil {
			return err
		}
		return o.IO.Encode(cmd.OutOrStdout(), cmdio.NewSingleMutation("updated", cmdio.MutationTarget{Kind: "SCIMConfig", Name: updated.GetName(), Namespace: updated.GetNamespace()}))
	}}
	o.setup(cmd.Flags(), true)
	return cmd
}

// safeConfig selects public settings. Unknown fields can contain credentials.
func safeConfig(item *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	safe := &unstructured.Unstructured{Object: map[string]any{"apiVersion": item.GetAPIVersion(), "kind": item.GetKind(), "metadata": map[string]any{"name": item.GetName(), "namespace": item.GetNamespace(), "resourceVersion": item.GetResourceVersion()}, "spec": map[string]any{}}}
	for _, key := range []string{"enableUserSync", "enableGroupSync", "rejectNonProvisionedUsers"} {
		value, found, err := unstructured.NestedBool(item.Object, "spec", key)
		if err != nil {
			return nil, fmt.Errorf("invalid SCIM setting %s: expected a boolean", key)
		}
		if found {
			if err := unstructured.SetNestedField(safe.Object, value, "spec", key); err != nil {
				return nil, err
			}
		}
	}
	return safe, nil
}

func newEndpointCommand(binding native.Binding) *cobra.Command {
	parent := &cobra.Command{Use: "endpoint", Short: "Show the SCIM tenant endpoint"}
	o := &configOptions{}
	cmd := &cobra.Command{Use: "get", Short: "Get the SCIM tenant base URL", Long: "Get the SCIM tenant base URL from the active Grafana server and the namespace returned by the SCIM config API. No token is included.", Args: cobra.NoArgs, Example: "  gcx scim endpoint get -o json", RunE: func(cmd *cobra.Command, _ []string) error {
		if err := o.Validate(cmd, false); err != nil {
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
		return o.IO.Encode(cmd.OutOrStdout(), map[string]string{"url": endpoint})
	}}
	o.setup(cmd.Flags(), false)
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
