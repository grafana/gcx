package azure

import (
	"context"
	"fmt"
	"strings"

	"github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/onboard"
)

// RotateInput scopes and controls a secret-rotation sweep.
type RotateInput struct {
	// CallerOID and Stack restrict rotation to app registrations attributable to
	// this caller/stack (verified against the gcx-managed tags), so a shared
	// tenant never rotates another caller's or stack's credentials.
	CallerOID string
	Stack     string
	// ExpiryDays, when > 0, sets an explicit end date on the newly minted
	// secret.
	ExpiryDays int
	// DryRun lists what would be rotated without minting or updating anything.
	DryRun bool
	// SkipHealth skips the post-rotation datasource health verification.
	SkipHealth bool
	// IncludeUIDs, when non-empty, restricts rotation to datasources with these
	// UIDs (used by the interactive picker). Nil/empty means every gcx-managed
	// Azure datasource is a candidate.
	IncludeUIDs []string
}

// Rotate mints a fresh client secret for each gcx-managed Azure datasource,
// updates the datasource to use it, and retires the superseded secret. It only
// touches datasources whose backing app registration is tagged gcx-managed and
// attributable to this caller/stack; key-based datasources (Cosmos DB) and
// datasources using supplied credentials are reported as skipped.
func Rotate(ctx context.Context, deps RunDeps, in RotateInput) (onboard.Result, error) {
	// A real rotation mints and prunes secrets, so it must be scoped to the
	// caller. Without a caller OID, attributableToCaller degrades to "any
	// gcx-managed app", which in a shared tenant would rotate (and prune) other
	// owners' credentials. Refuse rather than widen the sweep. --dry-run is
	// read-only and stays allowed.
	if !in.DryRun && in.CallerOID == "" {
		return onboard.Result{}, fmt.Errorf(
			"%w: no signed-in Azure user object ID was resolved (e.g. a service-principal or managed-identity login); re-run as a user, or preview with --dry-run",
			ErrUnscopedDestructive)
	}

	prefix := onboard.NamePrefix + "-"

	only := uidSet(in.IncludeUIDs)

	onboard.Progressf(deps.Progress, "Listing gcx-created Grafana datasources...")
	list, err := deps.DS.List(ctx)
	if err != nil {
		return onboard.Result{}, err
	}

	results := make([]onboard.DatasourceResult, 0)
	for _, item := range list {
		if !strings.HasPrefix(item.Name, prefix) {
			continue
		}
		if only != nil && !only[item.UID] {
			continue
		}

		// The /api/datasources list omits jsonData, so fetch the full datasource
		// to read the backing client ID and to preserve jsonData on update.
		d, err := deps.DS.GetByUID(ctx, item.UID)
		if err != nil {
			warn(deps.ErrOut, fmt.Sprintf("skipping %q: could not read datasource details: %v", item.Name, err))
			results = append(results, skipRotate(item, "could not read datasource details"))
			continue
		}

		appID, field := credentialRef(d)
		if appID == "" {
			results = append(results, skipRotate(d, "no rotatable service-principal credential"))
			continue
		}

		tags, err := deps.CLI.AppTags(ctx, appID)
		if err != nil {
			warn(deps.ErrOut, fmt.Sprintf("skipping %q: could not read app registration tags: %v", d.Name, err))
			results = append(results, skipRotate(d, "could not verify gcx ownership"))
			continue
		}
		if !attributableToCaller(tags, in.CallerOID, in.Stack) {
			results = append(results, skipRotate(d, "app registration is not gcx-managed by this caller/stack"))
			continue
		}

		if in.DryRun {
			r := skipRotate(d, "would rotate secret")
			r.Status = onboard.StatusPlanned
			r.Credential = &onboard.CloudCredential{ID: appID}
			results = append(results, r)
			continue
		}

		res, err := rotateOne(ctx, deps, in, d, appID, field)
		if err != nil {
			return onboard.Result{}, err
		}
		results = append(results, res)
	}

	return onboard.Result{Provider: "azure", Datasources: results, DryRun: in.DryRun}, nil
}

// rotateOne rotates the secret for a single datasource: mint a new secret,
// update the datasource to use it, then prune the superseded secret.
func rotateOne(
	ctx context.Context,
	deps RunDeps,
	in RotateInput,
	d *datasources.Datasource,
	appID, field string,
) (onboard.DatasourceResult, error) {
	onboard.Progressf(deps.Progress, "→ rotating secret for %q...", d.Name)
	rot, err := deps.CLI.RotateSecret(ctx, appID, in.ExpiryDays)
	if err != nil {
		return onboard.DatasourceResult{}, fmt.Errorf("failed to rotate secret for %q: %w", d.Name, err)
	}

	// Update the datasource to use the new secret before retiring the old one,
	// so a failure here leaves the still-valid previous secret in place.
	update := *d
	update.SecureJSONData = map[string]string{field: rot.Secret}
	update.SecureJSONFields = nil
	if _, err := deps.DS.Update(ctx, d.UID, &update); err != nil {
		return onboard.DatasourceResult{}, fmt.Errorf(
			"rotated the credential for app %q but failed to update datasource %q; the previous secret is still valid: %w",
			appID, d.Name, err)
	}

	// Retire the superseded secret(s). Best-effort: the datasource already uses
	// the new secret, so a prune failure is a warning, not a run failure.
	if err := deps.CLI.PruneSecretsExcept(ctx, appID, rot.KeyID); err != nil {
		warn(deps.ErrOut, fmt.Sprintf("rotated %q but could not remove the old secret(s): %v", d.Name, err))
	}
	onboard.Progressf(deps.Progress, "  rotated secret for %q", d.Name)

	result := onboard.DatasourceResult{
		Name: d.Name, Type: d.Type, UID: d.UID, Status: onboard.StatusRotated,
		Credential: &onboard.CloudCredential{ID: appID},
	}
	if !in.SkipHealth {
		applyHealth(ctx, deps, &result, d.UID)
	}
	return result, nil
}

// skipRotate builds a skipped result row with a note.
func skipRotate(d *datasources.Datasource, note string) onboard.DatasourceResult {
	return onboard.DatasourceResult{
		Name: d.Name, Type: d.Type, UID: d.UID, Status: onboard.StatusSkipped, Note: note,
	}
}

// credentialRef returns the app (client) ID backing a gcx datasource and the
// secureJsonData field that holds its client secret, based on the datasource
// type. It returns ("", "") for datasources without a rotatable
// service-principal secret (e.g. key-based Cosmos DB or an unknown type).
//
// The client ID is read from whichever schema the datasource uses: gcx writes
// the flat Azure Monitor schema (top-level clientId), but Grafana may normalize
// it into the shared azureCredentials object, so both are checked.
func credentialRef(d *datasources.Datasource) (string, string) {
	switch d.Type {
	case KindAzureMonitor:
		id := stringField(d.JSONData, "clientId")
		if id == "" {
			id = stringField(azureCredentials(d), "clientId")
		}
		if id == "" {
			return "", ""
		}
		return id, azureSecretField(d, "clientSecret")
	case KindADX:
		id := stringField(azureCredentials(d), "clientId")
		if id == "" {
			return "", ""
		}
		return id, azureSecretField(d, "azureClientSecret")
	default:
		return "", ""
	}
}

// azureCredentials extracts the nested azureCredentials object from a
// datasource's jsonData, or nil when absent.
func azureCredentials(d *datasources.Datasource) map[string]any {
	if creds, ok := d.JSONData["azureCredentials"].(map[string]any); ok {
		return creds
	}
	return nil
}

// azureSecretField returns the secureJsonData key currently holding the client
// secret, preferring whichever key the datasource already reports as set so
// rotation overwrites the exact field Grafana expects. It falls back to the
// type's default key when nothing is reported.
func azureSecretField(d *datasources.Datasource, fallback string) string {
	for _, k := range []string{"azureClientSecret", "clientSecret"} {
		if d.SecureJSONFields[k] {
			return k
		}
	}
	return fallback
}

// uidSet builds a lookup set from the include list, or nil when empty (meaning
// "no restriction").
func uidSet(uids []string) map[string]bool {
	if len(uids) == 0 {
		return nil
	}
	set := make(map[string]bool, len(uids))
	for _, u := range uids {
		set[u] = true
	}
	return set
}

func stringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}
