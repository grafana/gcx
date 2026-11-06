// opmodel_entities.go; `gcx irm opmodel containers update-entity-text`: reword an
// observation, claim, or action. Only the text changes, and the event stream
// preserves the previous wording.
package irm

import (
	"context"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type entitiesEditTextOpts struct {
	IO        cmdio.Options
	Container string
	Text      string
	Actor     opModelActorOpts
}

func (o *entitiesEditTextOpts) setup(flags *pflag.FlagSet) {
	flags.StringVar(&o.Text, "text", "", "The new wording (required)")
	o.Actor.bind(flags)
	setupOpModelMutationIO(&o.IO, flags)
}

func (o *entitiesEditTextOpts) Validate() error {
	if err := requireOpModelArg("container", o.Container); err != nil {
		return err
	}
	if err := requireOpModelFlag("text", o.Text); err != nil {
		return err
	}
	return o.Actor.validate()
}

func newEntitiesEditTextCmd(loader OpModelClientLoader, shared *opModelSharedOpts) *cobra.Command {
	opts := &entitiesEditTextOpts{}
	cmd := &cobra.Command{
		Use:   "update-entity-text <container> <entity-id>",
		Short: "Reword an observation, claim, or action (history stays in the stream).",
		Long: `Reword an entity: an observation, a claim, or an action, addressed by
id. This changes only wording and preserves optional content fields.
The previous wording stays in the event stream.`,
		Example: `  gcx irm opmodel containers update-entity-text INC-123 obs-42 \
    --text "p99 latency doubled at 14:02 UTC (was: 14:02 local)" \
    --actor-kind human --actor-id deric`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Container = args[0]
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			return runEntitiesEditText(cmd, args[1], opts, loader, shared)
		},
	}
	opts.setup(cmd.Flags())
	return markExperimental(cmd)
}

func runEntitiesEditText(cmd *cobra.Command, entityID string, opts *entitiesEditTextOpts, loader OpModelClientLoader, shared *opModelSharedOpts) error {
	if err := validateOpModelIDInputs(shared, "entity-id", entityID, opts); err != nil {
		return err
	}
	return runOpModelMutation(cmd, loader, shared, &opts.IO, func(ctx context.Context, api OpModelAPI) (opModelMutation, error) {
		resp, err := api.EditText(ctx, editTextRequest{
			Container: opts.Container,
			Actor:     opts.Actor.actor(),
			EntityID:  entityID,
			Text:      opts.Text,
		})
		if err != nil {
			return opModelMutation{}, err
		}
		return newOpModelMutation("update-entity-text", opts.Container,
			opModelTarget{Kind: "entity", ID: entityID},
			nil, resp.Revision), nil
	})
}
