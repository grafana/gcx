package azure

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/agent"
	dsclient "github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/onboard"
	azonboard "github.com/grafana/gcx/internal/onboard/azure"
	cmdio "github.com/grafana/gcx/internal/output"
	pluginsclient "github.com/grafana/gcx/internal/plugins"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/spf13/cobra"
)

func runAzure(cmd *cobra.Command, opts *azureOpts) error {
	ctx := cmd.Context()
	log := logging.FromContext(ctx)
	errOut := cmd.ErrOrStderr()

	// Progress narration goes to stderr for humans; suppressed in agent mode
	// where the structured result on stdout is the contract.
	var progress io.Writer
	if !agent.IsAgentMode() {
		progress = errOut
	}

	onboard.Progressf(progress, "Checking for the Azure CLI (az)...")
	cli := azonboard.NewCLI()
	if err := cli.Ensure(); err != nil {
		return err
	}

	onboard.Progressf(progress, "Connecting to Grafana...")
	restCfg, err := opts.Config.LoadGrafanaConfig(ctx)
	if err != nil {
		return err
	}
	ds, err := dsclient.NewClient(restCfg)
	if err != nil {
		return err
	}
	pluginsClient, err := pluginsclient.NewClient(restCfg)
	if err != nil {
		return err
	}

	deps := azonboard.RunDeps{CLI: cli, DS: ds, Plugins: pluginsClient, Log: log, ErrOut: errOut, Progress: progress}
	stack := stackLabel(restCfg)

	if opts.Cleanup {
		if opts.DryRun {
			onboard.Progressf(progress, "Previewing gcx-created Azure artifacts and datasources that would be removed...")
		} else {
			onboard.Progressf(progress, "Removing gcx-created Azure artifacts and datasources...")
		}
		// Resolve the caller identity so cleanup only touches app registrations
		// attributable to this caller (best-effort: an unresolved OID falls back
		// to the gcx-managed tag guard alone).
		callerOID, err := cli.SignedInUserObjectID(ctx)
		if err != nil {
			return err
		}
		res, err := azonboard.Cleanup(ctx, deps, azonboard.CleanupInput{
			CallerOID: callerOID,
			Stack:     stack,
			DryRun:    opts.DryRun,
		})
		if err != nil {
			return err
		}
		return opts.IO.Encode(cmd.OutOrStdout(), res)
	}

	interactive := isInteractive(opts)

	// In interactive mode, ask before reverting partial work on failure and
	// before installing a missing required plugin.
	if interactive {
		deps.ConfirmRollback = func(steps []string) (bool, error) {
			return confirmRollback(errOut, steps)
		}
		deps.ConfirmInstallPlugin = confirmInstallPlugin
	}

	// Resolve target subscriptions.
	onboard.Progressf(progress, "Discovering Azure subscriptions...")
	subs, err := resolveSubscriptions(ctx, cli, opts, interactive)
	if err != nil {
		return err
	}
	if len(subs) == 0 {
		cmdio.EmitNote(errOut, "no subscriptions selected; nothing to do")
		return opts.IO.Encode(cmd.OutOrStdout(), onboard.Result{Provider: "azure"})
	}

	// Resolve the caller object ID once (owner for minted artifacts). Tolerate
	// failure when only previewing (--dry-run), which mints nothing that needs
	// an owner.
	onboard.Progressf(progress, "Resolving your Azure identity...")
	callerOID, oidErr := cli.SignedInUserObjectID(ctx)
	if oidErr != nil && !opts.DryRun {
		return oidErr
	}

	// Preserve and restore the originally-active subscription.
	original, _ := cli.CurrentAccount(ctx)
	defer func() {
		if original.SubID != "" {
			_ = cli.SetSubscription(context.WithoutCancel(ctx), original.SubID)
		}
	}()

	// Provision each subscription independently. A failure in one subscription
	// is recorded but does not discard the artifacts already created in earlier
	// subscriptions — those are always reported (fixing the cross-subscription
	// orphan gap).
	combined := onboard.Result{Provider: "azure", DryRun: opts.DryRun}
	var subErrs []error
	for _, sub := range subs {
		res, err := provisionSubscription(ctx, deps, cli, opts, sub, original, stack, callerOID, interactive, errOut, log)
		combined.Datasources = append(combined.Datasources, res.Datasources...)
		if err != nil {
			subErrs = append(subErrs, fmt.Errorf("subscription %q: %w", subLabel(sub), err))
		}
	}

	// Always surface what was created, even when a later subscription failed.
	if encErr := opts.IO.Encode(cmd.OutOrStdout(), combined); encErr != nil {
		return encErr
	}
	if len(subErrs) > 0 {
		return partialFailureError(combined, subErrs)
	}
	return nil
}

// provisionSubscription switches to the subscription, discovers candidates, and
// provisions the resolved selections. It returns whatever was created together
// with any error, so the caller can report partial success across
// subscriptions.
func provisionSubscription(
	ctx context.Context,
	deps azonboard.RunDeps,
	cli *azonboard.CLI,
	opts *azureOpts,
	sub, original azonboard.Account,
	stack, callerOID string,
	interactive bool,
	errOut io.Writer,
	log logging.Logger,
) (onboard.Result, error) {
	if err := cli.SetSubscription(ctx, sub.SubID); err != nil {
		return onboard.Result{}, err
	}
	acct := normalizeAccount(sub, original)

	onboard.Progressf(deps.Progress, "Scanning subscription %q for datasources...", subLabel(sub))
	suggestions := azonboard.BuildPlan(ctx, azonboard.PlanInput{
		CLI:           cli,
		Stack:         stack,
		Account:       acct,
		IncludeCosmos: opts.IncludeCosmos,
		Log:           log,
	})
	onboard.Progressf(deps.Progress, "  found %d candidate datasource(s)", len(suggestions))

	selections, err := resolveSelections(suggestions, opts, interactive, errOut)
	if err != nil {
		return onboard.Result{}, err
	}
	if len(selections) == 0 {
		return onboard.Result{}, nil
	}

	return provisionGuarded(ctx, deps, azonboard.ProvisionInput{
		Account:     acct,
		CallerOID:   callerOID,
		Selections:  selections,
		Interactive: interactive,
		Stack:       stack,
		ExpiryDays:  opts.SecretExpiry,
		DryRun:      opts.DryRun,
		SkipHealth:  opts.SkipHealth,
	})
}

// provisionGuarded runs Provision and maps an insufficient-privilege failure to
// an actionable DetailedError explaining the directory and RBAC roles gcx needs
// to mint app registrations and assign roles.
func provisionGuarded(ctx context.Context, deps azonboard.RunDeps, in azonboard.ProvisionInput) (onboard.Result, error) {
	res, err := azonboard.Provision(ctx, deps, in)
	if err == nil {
		return res, nil
	}
	if !errors.Is(err, azonboard.ErrInsufficientPrivilege) {
		return onboard.Result{}, err
	}
	return onboard.Result{}, gcxerrors.DetailedError{
		Summary: "insufficient Azure privileges to mint app registrations or assign roles",
		Details: "gcx needs directory privileges (e.g. Application Developer) to create app registrations, and Owner/User Access Administrator to assign roles.",
		Parent:  err,
		Suggestions: []string{
			"Ask an administrator to grant the required roles, then re-run",
		},
	}
}

// partialFailureError wraps per-subscription errors, noting how many
// datasources were still created/verified so the user is not left thinking the
// whole run was lost.
func partialFailureError(combined onboard.Result, subErrs []error) error {
	created := 0
	for _, d := range combined.Datasources {
		if d.Status == onboard.StatusCreated || d.Status == onboard.StatusExisting {
			created++
		}
	}
	details := ""
	if created > 0 {
		details = fmt.Sprintf("%d datasource(s) were created or already existed and are reported above; the failure affected other subscriptions only.", created)
	}
	return gcxerrors.DetailedError{
		Summary: "one or more subscriptions failed during onboarding",
		Details: details,
		Parent:  errors.Join(subErrs...),
		Suggestions: []string{
			"Review the errors below and re-run for the affected subscription(s) with --subscription <id>",
			"Re-running is safe: already-created datasources are detected and reused, not duplicated",
		},
	}
}
