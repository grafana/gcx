package cloud

import (
	"errors"
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/agent"
	cloudapi "github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type orgListOpts struct{ IO cmdio.Options }

func (o *orgListOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &orgTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func (o *orgListOpts) Validate() error { return o.IO.Validate() }

func orgsCommand() *cobra.Command {
	loader := &providers.ConfigLoader{}
	parent := &cobra.Command{Use: "orgs", Short: "Discover your Grafana Cloud organisations"}
	loader.BindFlags(parent.PersistentFlags())
	opts := &orgListOpts{}
	cmd := &cobra.Command{
		Use: "list", Short: "List the signed-in user's Grafana Cloud organisation memberships.",
		Long: `List all organisation memberships returned by the Grafana Cloud OAuth API.
Requires a browser Cloud login with the profile scope. Cloud access-policy tokens
cannot enumerate user memberships. Existing logins may need re-authentication:
  gcx cloud login
The default scopes include profile and stack management. Access-policy tokens
from GRAFANA_CLOUD_TOKEN or cloud.<entry>.token take precedence over OAuth;
unset them when using this command with a browser login.

Returns organisation slugs and membership roles, not names or numeric IDs.
Membership does not guarantee permission to create stacks. To list stacks within
an organisation, use gcx cloud stacks list --org <slug>.`,
		Example: "  gcx cloud orgs list\n  gcx cloud orgs list -o json\n  gcx cloud orgs list --json slug,role",
		Args:    cobra.NoArgs,
		Annotations: map[string]string{
			agent.AnnotationRequiredScope: "profile",
			agent.AnnotationTokenCost:     "small",
			agent.AnnotationLLMHint:       "Discover Cloud organisation slugs for the signed-in user. Requires Cloud browser OAuth with profile scope; not a Grafana instance org list or access-policy token inventory.",
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			cfg, err := loader.LoadCloudTokenConfig(cmd.Context())
			if err != nil {
				return err
			}
			orgs, err := cfg.Client.ListOrgs(cmd.Context())
			if err != nil {
				return fmt.Errorf("failed to list cloud organisations: %w", err)
			}
			return opts.IO.Encode(cmd.OutOrStdout(), orgs)
		},
	}
	opts.setup(cmd.Flags())
	parent.AddCommand(cmd)
	return parent
}

type orgTableCodec struct{}

func (*orgTableCodec) Format() format.Format { return "table" }
func (*orgTableCodec) Encode(w io.Writer, v any) error {
	orgs, ok := v.([]cloudapi.OrgMembership)
	if !ok {
		return errors.New("invalid organisation table data")
	}
	table := style.NewTable("SLUG", "ROLE")
	for _, org := range orgs {
		table.Row(org.Slug, org.Role)
	}
	return table.Render(w)
}

func (*orgTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}
