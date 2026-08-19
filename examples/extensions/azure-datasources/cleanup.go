package main

import (
	"context"
	"io"
	"strings"
)

// removedArtifact is one object cleanup deleted, or would delete in --dry-run.
type removedArtifact struct {
	Kind    string `json:"kind"` // "datasource" or "app-registration"
	Name    string `json:"name"`
	ID      string `json:"id,omitempty"`
	Planned bool   `json:"planned,omitempty"`
	Error   string `json:"error,omitempty"`
}

// deleteStatus is one row of `gcx datasources delete --output json`.
type deleteStatus struct {
	UID     string `json:"uid"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// deleteMessage prefers gcx's own per-item message over the generic exit-code
// error, so the user sees "missing scope", not "exit status 4".
func deleteMessage(items []deleteStatus, uid string, err error) string {
	for _, item := range items {
		if item.UID == uid && item.Message != "" {
			return item.Message
		}
	}
	return err.Error()
}

// cleanup removes the Grafana datasources and Azure app registrations this
// extension created, matched by the prefix it stamps on both.
func cleanup(ctx context.Context, o *options, g *gcxClient, az azCLI, progress io.Writer) (*runResult, error) {
	if err := az.ensure(); err != nil {
		return nil, err
	}

	result := &runResult{Context: g.currentContext(ctx), DryRun: o.dryRun}

	datasources, err := g.listDatasources(ctx)
	if err != nil {
		return nil, err
	}
	for _, ds := range datasources {
		if !strings.HasPrefix(ds.UID, artifactPrefix) {
			continue
		}
		entry := removedArtifact{Kind: "datasource", Name: ds.Name, ID: ds.UID, Planned: o.dryRun}
		if !o.dryRun {
			progressf(progress, "Deleting datasource %s...", ds.Name)
			// Force JSON: on a partial failure gcx reports the reason in its
			// result document on stdout, and the human codec renders it as a
			// table an extension cannot read. Note the envelope differs from
			// `datasources list`: delete returns a bare array.
			var deleted []deleteStatus
			if err := g.json(ctx, &deleted, "datasources", "delete", ds.UID, "--yes"); err != nil {
				entry.Error = deleteMessage(deleted, ds.UID, err)
			}
		}
		result.Removed = append(result.Removed, entry)
	}

	apps, err := az.listOwnedApps(ctx)
	if err != nil {
		return result, err
	}
	for _, app := range apps {
		entry := removedArtifact{Kind: "app-registration", Name: app.DisplayName, ID: app.AppID, Planned: o.dryRun}
		if !o.dryRun {
			progressf(progress, "Deleting Azure app registration %s...", app.DisplayName)
			if err := az.deleteApp(ctx, app.AppID); err != nil {
				entry.Error = err.Error()
			}
		}
		result.Removed = append(result.Removed, entry)
	}

	return result, nil
}
