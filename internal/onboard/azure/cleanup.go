package azure

import (
	"context"
	"io"
	"strings"

	"github.com/grafana/gcx/internal/onboard"
	"github.com/grafana/gcx/internal/output"
)

// CleanupInput scopes and controls a cleanup sweep.
type CleanupInput struct {
	// CallerOID, when set, restricts app-registration removal to artifacts
	// tagged with this owner object ID, so a shared tenant never has one user's
	// cleanup delete another user's app registrations.
	CallerOID string
	// Stack, when set, restricts app-registration removal to artifacts tagged
	// with this Grafana stack slug.
	Stack string
	// DryRun lists what would be removed without deleting anything.
	DryRun bool
}

// Cleanup removes gcx-created artifacts: datasources whose name starts with the
// gcx- prefix, and app registrations that are tagged gcx-managed (and, when a
// caller/stack is known, owned by this caller and scoped to this stack). It is
// best-effort — individual failures are warned and skipped. With DryRun set it
// reports what would be removed without deleting anything.
func Cleanup(ctx context.Context, deps RunDeps, in CleanupInput) (onboard.Result, error) {
	prefix := onboard.NamePrefix + "-"
	var cleaned []onboard.CleanupResult

	progressf(deps.Progress, "Listing gcx-created Grafana datasources...")
	list, err := deps.DS.List(ctx)
	if err != nil {
		return onboard.Result{}, err
	}
	for _, d := range list {
		if !strings.HasPrefix(d.Name, prefix) {
			continue
		}
		if in.DryRun {
			cleaned = append(cleaned, onboard.CleanupResult{Kind: "datasource", Name: d.Name, ID: d.UID, Planned: true})
			continue
		}
		progressf(deps.Progress, "  deleting datasource %q...", d.Name)
		if err := deps.DS.Delete(ctx, d.UID); err != nil {
			warn(deps.ErrOut, "failed to delete datasource "+d.Name+": "+err.Error())
			continue
		}
		cleaned = append(cleaned, onboard.CleanupResult{Kind: "datasource", Name: d.Name, ID: d.UID})
	}

	// Remove gcx-created ADX cluster principal assignments. These are scoped to
	// the active subscription's clusters and outlive the app registration, so
	// clean them before deleting the apps. Best-effort: discovery failures (e.g.
	// no kusto extension or no ADX clusters) are warned and skipped.
	cleaned = append(cleaned, cleanupADXAssignments(ctx, deps, prefix, in.DryRun)...)

	progressf(deps.Progress, "Listing gcx-created Azure app registrations...")
	apps, err := deps.CLI.ListAppsByPrefix(ctx, prefix)
	if err != nil {
		return onboard.Result{}, err
	}
	for _, a := range apps {
		if !ownedByCaller(a, in) {
			if deps.Log != nil {
				deps.Log.Debug("skipping app registration not attributable to this caller/stack",
					"appId", a.AppID, "displayName", a.DisplayName)
			}
			continue
		}
		if in.DryRun {
			cleaned = append(cleaned, onboard.CleanupResult{Kind: "app-registration", Name: a.DisplayName, ID: a.AppID, Planned: true})
			continue
		}
		progressf(deps.Progress, "  deleting app registration %q...", a.DisplayName)
		if err := deps.CLI.DeleteAppRegistration(ctx, a.AppID); err != nil {
			warn(deps.ErrOut, "failed to delete app registration "+a.DisplayName+": "+err.Error())
			continue
		}
		cleaned = append(cleaned, onboard.CleanupResult{Kind: "app-registration", Name: a.DisplayName, ID: a.AppID})
	}

	return onboard.Result{Provider: "azure", Cleaned: cleaned, DryRun: in.DryRun}, nil
}

// ownedByCaller reports whether an app registration is safe for this caller to
// remove: it must carry the gcx-managed tag, and — when the caller object ID
// and/or stack are known — its owner/stack tags must match. This guards a
// shared tenant against one caller's cleanup deleting another caller's or
// stack's app registrations.
func ownedByCaller(a AppSummary, in CleanupInput) bool {
	return attributableToCaller(a.Tags, in.CallerOID, in.Stack)
}

// cleanupADXAssignments removes gcx-prefixed cluster principal assignments from
// all ADX clusters in the active subscription.
func cleanupADXAssignments(ctx context.Context, deps RunDeps, prefix string, dryRun bool) []onboard.CleanupResult {
	progressf(deps.Progress, "Listing ADX clusters for gcx-created assignments...")
	clusters, err := deps.CLI.ListKustoClusters(ctx)
	if err != nil {
		warn(deps.ErrOut, "skipping ADX assignment cleanup: "+err.Error())
		return nil
	}

	var cleaned []onboard.CleanupResult
	for _, cl := range clusters {
		assigns, err := deps.CLI.ListADXClusterAssignments(ctx, cl.RG, cl.Name)
		if err != nil {
			warn(deps.ErrOut, "skipping assignments on cluster "+cl.Name+": "+err.Error())
			continue
		}
		for _, a := range assigns {
			short := adxAssignmentShortName(a.Name)
			if !strings.HasPrefix(short, prefix) {
				continue
			}
			if dryRun {
				cleaned = append(cleaned, onboard.CleanupResult{Kind: "adx-assignment", Name: short, ID: cl.Name, Planned: true})
				continue
			}
			progressf(deps.Progress, "  deleting ADX assignment %q on cluster %q...", short, cl.Name)
			if err := deps.CLI.DeleteADXClusterAssignment(ctx, cl.RG, cl.Name, short); err != nil {
				warn(deps.ErrOut, "failed to delete ADX assignment "+short+": "+err.Error())
				continue
			}
			cleaned = append(cleaned, onboard.CleanupResult{Kind: "adx-assignment", Name: short, ID: cl.Name})
		}
	}
	return cleaned
}

func warn(w io.Writer, msg string) {
	if w != nil {
		output.EmitWarn(w, msg)
	}
}
