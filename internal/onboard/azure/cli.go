// Package azure implements `gcx setup datasources azure`: it discovers Azure resources
// via the local `az` CLI, mints gcx-owned app registrations per suggested
// datasource, and provisions the matching Grafana datasource.
package azure

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/cloudcli"
	"github.com/grafana/grafana-app-sdk/logging"
)

const azInstallDocs = "https://learn.microsoft.com/cli/azure/install-azure-cli"

// Attribution tag keys applied to gcx-created app registrations. They make
// gcx-owned artifacts reliably discoverable and let --cleanup/rotate scope to
// this caller and stack instead of matching on the fragile name prefix alone.
const (
	// TagManaged marks an app registration as created and owned by gcx.
	TagManaged = "gcx:managed"
	// tagStackPrefix is followed by the Grafana stack slug the artifact serves.
	tagStackPrefix = "gcx:stack="
	// tagOwnerPrefix is followed by the object ID of the caller who created it.
	tagOwnerPrefix = "gcx:owner="
	// tagDatasourcePrefix is followed by the UID of the datasource it backs.
	tagDatasourcePrefix = "gcx:datasource-uid="
)

// Attribution carries the metadata gcx stamps onto created artifacts (tags on
// app registrations, descriptions on role assignments) so they can be
// attributed to gcx, this caller, and a specific Grafana datasource.
type Attribution struct {
	Stack         string // Grafana stack slug (may be empty)
	OwnerOID      string // caller object ID
	DatasourceUID string // UID of the datasource the credential backs
}

// Tags renders the attribution as the app-registration tag set.
func (a Attribution) Tags() []string {
	tags := []string{TagManaged}
	if a.Stack != "" {
		tags = append(tags, tagStackPrefix+a.Stack)
	}
	if a.OwnerOID != "" {
		tags = append(tags, tagOwnerPrefix+a.OwnerOID)
	}
	if a.DatasourceUID != "" {
		tags = append(tags, tagDatasourcePrefix+a.DatasourceUID)
	}
	return tags
}

// roleDescription renders a human-readable role-assignment description used for
// attribution on `az role assignment create`.
func (a Attribution) roleDescription(datasourceName string) string {
	parts := []string{"created-by=gcx", "datasource=" + datasourceName}
	if a.Stack != "" {
		parts = append(parts, "stack="+a.Stack)
	}
	if a.OwnerOID != "" {
		parts = append(parts, "owner="+a.OwnerOID)
	}
	return strings.Join(parts, "; ")
}

// hasManagedTag reports whether the given tag set marks a gcx-managed artifact.
func hasManagedTag(tags []string) bool {
	return slices.Contains(tags, TagManaged)
}

// tagValue returns the value of the first tag with the given prefix, or "".
func tagValue(tags []string, prefix string) string {
	for _, t := range tags {
		if v, ok := strings.CutPrefix(t, prefix); ok {
			return v
		}
	}
	return ""
}

// attributableToCaller reports whether an app registration with the given tags
// is safe for this caller/stack to act on (cleanup or rotate): it must be
// gcx-managed, and — when a caller object ID and/or stack slug are known — its
// owner/stack tags must not belong to a different caller or stack. This guards
// a shared tenant against cross-user or cross-stack mutation.
func attributableToCaller(tags []string, callerOID, stack string) bool {
	if !hasManagedTag(tags) {
		return false
	}
	if callerOID != "" {
		if owner := tagValue(tags, tagOwnerPrefix); owner != "" && owner != callerOID {
			return false
		}
	}
	if stack != "" {
		if s := tagValue(tags, tagStackPrefix); s != "" && s != stack {
			return false
		}
	}
	return true
}

// ErrInsufficientPrivilege indicates the signed-in `az` identity lacks the
// directory or RBAC permissions required to mint an app registration, assign a
// role, or grant ADX access. The command layer maps this to an actionable
// error explaining the roles an administrator must grant.
var ErrInsufficientPrivilege = errors.New("insufficient Azure privileges")

// CLI is a thin wrapper around the `az` binary.
type CLI struct {
	tool cloudcli.Tool
}

// NewCLI returns a CLI backed by the real `az` binary.
func NewCLI() *CLI {
	return &CLI{tool: cloudcli.New("az", "Azure CLI", azInstallDocs)}
}

// NewCLIWithRunner returns a CLI backed by an injected runner (tests).
func NewCLIWithRunner(r cloudcli.Runner) *CLI {
	return &CLI{tool: cloudcli.New("az", "Azure CLI", azInstallDocs).WithRunner(r)}
}

// Ensure verifies the `az` binary is available.
func (c *CLI) Ensure() error { return c.tool.Ensure() }

// Account holds the fields from `az account show`/`az account list`.
type Account struct {
	TenantID  string `json:"tenantId"`
	SubID     string `json:"id"`
	Name      string `json:"name"`
	CloudName string `json:"environmentName"` // AzureCloud / AzureUSGovernment / AzureChinaCloud
	IsDefault bool   `json:"isDefault"`
}

// CurrentAccount returns the active subscription (`az account show`).
func (c *CLI) CurrentAccount(ctx context.Context) (Account, error) {
	var a Account
	if err := c.tool.RunJSON(ctx, &a, "account", "show", "-o", "json"); err != nil {
		return Account{}, err
	}
	return a, nil
}

// ListSubscriptions returns all enabled subscriptions (`az account list`).
func (c *CLI) ListSubscriptions(ctx context.Context) ([]Account, error) {
	var a []Account
	if err := c.tool.RunJSON(ctx, &a, "account", "list", "--only-show-errors", "-o", "json"); err != nil {
		return nil, err
	}
	return a, nil
}

// SetSubscription sets the active `az` subscription. Resource discovery
// (kusto/cosmos list) operates on the active subscription, so the orchestrator
// switches subscription before discovering and provisioning per subscription,
// then restores the original.
func (c *CLI) SetSubscription(ctx context.Context, subID string) error {
	if _, stderr, err := c.tool.Run(ctx, "account", "set", "--subscription", subID, "--only-show-errors"); err != nil {
		return azError("set active subscription "+subID, err, stderr)
	}
	return nil
}

// SignedInUserObjectID returns the object ID of the signed-in user, used to set
// app/SP ownership at creation time.
func (c *CLI) SignedInUserObjectID(ctx context.Context) (string, error) {
	var id string
	if err := c.tool.RunJSON(ctx, &id, "ad", "signed-in-user", "show", "--query", "id", "-o", "json"); err != nil {
		return "", classifyAuthErr(err)
	}
	return id, nil
}

// SPCredential carries the credentials a datasource needs to authenticate.
type SPCredential struct {
	AppID    string
	Password string
	Tenant   string
}

// AppExists reports whether an app registration with the given display name
// already exists in the directory.
func (c *CLI) AppExists(ctx context.Context, displayName string) (bool, error) {
	var ids []string
	err := c.tool.RunJSON(ctx, &ids,
		"ad", "app", "list", "--display-name", displayName,
		"--query", "[].appId", "--only-show-errors", "-o", "json")
	if err != nil {
		return false, classifyAuthErr(err)
	}
	return len(ids) > 0, nil
}

// AppSummary identifies a gcx-created app registration for cleanup and rotation.
type AppSummary struct {
	AppID       string   `json:"appId"`
	DisplayName string   `json:"displayName"`
	Tags        []string `json:"tags"`
}

// ListAppsByPrefix lists app registrations whose display name starts with the
// given prefix, including their tags so callers can further scope by
// gcx-managed/owner/stack attribution (used by --cleanup and rotate).
func (c *CLI) ListAppsByPrefix(ctx context.Context, prefix string) ([]AppSummary, error) {
	var apps []AppSummary
	filter := fmt.Sprintf("startswith(displayName,'%s')", prefix)
	err := c.tool.RunJSON(ctx, &apps,
		"ad", "app", "list", "--filter", filter,
		"--query", "[].{appId:appId,displayName:displayName,tags:tags}",
		"--only-show-errors", "-o", "json")
	if err != nil {
		return nil, classifyAuthErr(err)
	}
	return apps, nil
}

// AppTags returns the tags on the app registration identified by appID.
func (c *CLI) AppTags(ctx context.Context, appID string) ([]string, error) {
	var tags []string
	err := c.tool.RunJSON(ctx, &tags,
		"ad", "app", "show", "--id", appID,
		"--query", "tags", "--only-show-errors", "-o", "json")
	if err != nil {
		return nil, classifyAuthErr(err)
	}
	return tags, nil
}

// SetAppTags replaces the tags on the app registration identified by appID via
// Microsoft Graph (the `az ad app` surface has no direct tags flag). The app is
// addressed by its appId using the Graph alternate-key syntax so callers do not
// need the object ID.
func (c *CLI) SetAppTags(ctx context.Context, appID string, tags []string) error {
	body, err := json.Marshal(map[string]any{"tags": tags})
	if err != nil {
		return fmt.Errorf("marshal tags: %w", err)
	}
	uri := fmt.Sprintf("https://graph.microsoft.com/v1.0/applications(appId='%s')", appID)
	if _, stderr, err := c.tool.Run(ctx, "rest", "--method", "PATCH",
		"--uri", uri, "--headers", "Content-Type=application/json",
		"--body", string(body), "--only-show-errors"); err != nil {
		return azError("set tags on app registration "+appID, err, stderr)
	}
	logging.FromContext(ctx).Info("tagged app registration", "appId", appID, "tags", strings.Join(tags, ","))
	return nil
}

// AppRegistrationRequest carries the inputs for minting a gcx-owned app
// registration.
type AppRegistrationRequest struct {
	Name      string   // display name (also the datasource name)
	Roles     []string // Azure RBAC roles to assign over Scopes
	Scopes    []string // role-assignment scopes
	CallerOID string   // signed-in user object ID (owner)
	Tenant    string   // tenant ID echoed back on the credential
	// Description is attached to each role assignment for attribution.
	Description string
	// ExpiryDays, when > 0, sets an explicit end date on the minted client
	// secret. When 0 the Azure default lifetime applies.
	ExpiryDays int
	// AddUndo registers rollback steps for each created object. May be nil.
	AddUndo func(desc string, undo func(ctx context.Context) error)
}

// CreateOwnedAppRegistration builds an app registration + service principal with
// the caller set as owner AT CREATION TIME (each owner-add runs immediately
// after the object is created, as a required step), then assigns the requested
// roles over scopes and mints a client secret. We deliberately avoid the
// one-shot `az ad sp create-for-rbac` so ownership is never deferred.
//
// Every created object is registered with req.AddUndo so a caller can roll back
// on a later failure.
func (c *CLI) CreateOwnedAppRegistration(ctx context.Context, req AppRegistrationRequest) (SPCredential, error) {
	log := logging.FromContext(ctx)

	// 1. App registration.
	var app struct {
		AppID string `json:"appId"`
		ID    string `json:"id"`
	}
	if err := c.tool.RunJSON(ctx, &app, "ad", "app", "create",
		"--display-name", req.Name, "--only-show-errors", "-o", "json"); err != nil {
		return SPCredential{}, classifyAuthErr(err)
	}
	log.Info("created app registration", "name", req.Name, "appId", app.AppID, "objectId", app.ID)
	if req.AddUndo != nil {
		appID := app.AppID
		req.AddUndo("delete app registration "+req.Name, func(ctx context.Context) error {
			return c.DeleteAppRegistration(ctx, appID)
		})
	}

	// 2. Owner on the app registration — immediately, required. Creating the app
	//    already sets the signed-in user as owner, so tolerate the benign
	//    "already exists" case (idempotent).
	if _, stderr, err := c.tool.Run(ctx, "ad", "app", "owner", "add",
		"--id", app.AppID, "--owner-object-id", req.CallerOID, "--only-show-errors"); err != nil && !ownerAlreadyExists(stderr) {
		return SPCredential{}, azError("add owner to app registration "+req.Name, err, stderr)
	}
	log.Info("set app registration owner", "appId", app.AppID, "ownerObjectId", req.CallerOID)

	// 3. Service principal.
	var sp struct {
		ID string `json:"id"`
	}
	if err := c.tool.RunJSON(ctx, &sp, "ad", "sp", "create",
		"--id", app.AppID, "--only-show-errors", "-o", "json"); err != nil {
		return SPCredential{}, classifyAuthErr(err)
	}
	log.Info("created service principal", "appId", app.AppID, "spObjectId", sp.ID)

	// 4. Owner on the service principal — immediately, required.
	//    There is no native `az ad sp owner add`; use Graph via `az rest`.
	if err := c.addSPOwner(ctx, sp.ID, req.CallerOID); err != nil {
		return SPCredential{}, err
	}
	log.Info("set service principal owner", "spObjectId", sp.ID, "ownerObjectId", req.CallerOID)

	// 5. Role assignments over scopes, stamped with an attribution description.
	for _, role := range req.Roles {
		for _, scope := range req.Scopes {
			args := []string{"role", "assignment", "create",
				"--assignee", app.AppID, "--role", role, "--scope", scope, "--only-show-errors"}
			if req.Description != "" {
				args = append(args, "--description", req.Description)
			}
			if _, stderr, err := c.tool.Run(ctx, args...); err != nil {
				return SPCredential{}, azError(fmt.Sprintf("assign role %q on %s", role, scope), err, stderr)
			}
			log.Info("assigned role", "appId", app.AppID, "role", role, "scope", scope)
		}
	}

	// 6. Client secret (shown once).
	cred, err := c.resetSecret(ctx, app.AppID, req.ExpiryDays)
	if err != nil {
		return SPCredential{}, err
	}
	log.Info("minted client secret", "appId", app.AppID)

	return SPCredential{AppID: app.AppID, Password: cred.Password, Tenant: req.Tenant}, nil
}

// secretReset is the subset of `az ad app credential reset` output gcx needs.
type secretReset struct {
	Password string `json:"password"`
	KeyID    string `json:"keyId"`
}

// resetSecret appends a new client secret to the app registration and returns
// it. When expiryDays > 0 an explicit end date is set so the secret has a
// bounded lifetime.
func (c *CLI) resetSecret(ctx context.Context, appID string, expiryDays int) (secretReset, error) {
	args := []string{"ad", "app", "credential", "reset", "--id", appID, "--append", "--only-show-errors", "-o", "json"}
	if expiryDays > 0 {
		end := time.Now().UTC().AddDate(0, 0, expiryDays).Format("2006-01-02T15:04:05Z")
		args = append(args, "--end-date", end)
	}
	var cred secretReset
	if err := c.tool.RunJSON(ctx, &cred, args...); err != nil {
		return secretReset{}, classifyAuthErr(err)
	}
	return cred, nil
}

// RotateSecret mints a fresh client secret on the app registration (appending a
// new credential) and returns the new secret plus its key ID. The caller is
// responsible for updating the consuming datasource and then, once that
// succeeds, pruning the older secrets via PruneSecretsExcept.
func (c *CLI) RotateSecret(ctx context.Context, appID string, expiryDays int) (SPCredentialRotation, error) {
	cred, err := c.resetSecret(ctx, appID, expiryDays)
	if err != nil {
		return SPCredentialRotation{}, err
	}
	logging.FromContext(ctx).Info("rotated client secret", "appId", appID, "keyId", cred.KeyID)
	return SPCredentialRotation{Secret: cred.Password, KeyID: cred.KeyID}, nil
}

// SPCredentialRotation is the result of minting a fresh client secret: the new
// secret value and the key ID that identifies it (so the caller can prune the
// superseded credentials).
type SPCredentialRotation struct {
	Secret string
	KeyID  string
}

// appCredential is one password credential on an app registration.
type appCredential struct {
	KeyID string `json:"keyId"`
}

// PruneSecretsExcept deletes every password credential on the app registration
// except the one identified by keepKeyID. Best-effort: individual delete
// failures are returned joined so the caller can warn. It is used after a
// successful rotation to retire the superseded secret(s).
func (c *CLI) PruneSecretsExcept(ctx context.Context, appID, keepKeyID string) error {
	var creds []appCredential
	if err := c.tool.RunJSON(ctx, &creds, "ad", "app", "credential", "list",
		"--id", appID, "--only-show-errors", "-o", "json"); err != nil {
		return classifyAuthErr(err)
	}
	var errs []error
	for _, cr := range creds {
		if cr.KeyID == "" || cr.KeyID == keepKeyID {
			continue
		}
		if _, stderr, err := c.tool.Run(ctx, "ad", "app", "credential", "delete",
			"--id", appID, "--key-id", cr.KeyID, "--only-show-errors"); err != nil {
			errs = append(errs, azError("delete superseded secret "+cr.KeyID, err, stderr))
			continue
		}
		logging.FromContext(ctx).Info("pruned superseded client secret", "appId", appID, "keyId", cr.KeyID)
	}
	return errors.Join(errs...)
}

// addSPOwner adds an owner to a service principal via Microsoft Graph (az rest),
// since the CLI has no native `az ad sp owner add`.
//
// Creating the service principal (`az ad sp create`) already adds the signed-in
// user as an owner, so this POST commonly returns Request_BadRequest reporting
// the owner reference already exists. That is the desired end state, so we treat
// it as success (idempotent).
func (c *CLI) addSPOwner(ctx context.Context, spObjectID, ownerOID string) error {
	body := fmt.Sprintf(`{"@odata.id":"https://graph.microsoft.com/v1.0/directoryObjects/%s"}`, ownerOID)
	uri := "https://graph.microsoft.com/v1.0/servicePrincipals/" + spObjectID + "/owners/$ref"
	if _, stderr, err := c.tool.Run(ctx, "rest", "--method", "POST",
		"--uri", uri, "--headers", "Content-Type=application/json",
		"--body", body, "--only-show-errors"); err != nil {
		if ownerAlreadyExists(stderr) {
			return nil
		}
		return azError("add owner to service principal", err, stderr)
	}
	return nil
}

// ownerAlreadyExists reports whether a Graph "add owner" failure is the benign
// case where the owner reference is already present.
func ownerAlreadyExists(stderr []byte) bool {
	s := strings.ToLower(string(stderr))
	return strings.Contains(s, "already exist") && strings.Contains(s, "owners")
}

// GrantADXClusterViewer grants the app principal AllDatabasesViewer on a Kusto
// cluster via the control-plane API. This avoids enumerating databases and does
// not require Kusto data-plane admin rights (only ARM Owner/Contributor on the
// cluster). assignmentName must be unique within the cluster.
func (c *CLI) GrantADXClusterViewer(ctx context.Context, rg, cluster, assignmentName, appID, tenant string) error {
	if _, stderr, err := c.tool.Run(ctx, "kusto", "cluster-principal-assignment", "create",
		"--cluster-name", cluster, "--resource-group", rg,
		"--principal-assignment-name", assignmentName,
		"--principal-id", appID, "--principal-type", "App",
		"--role", "AllDatabasesViewer", "--tenant-id", tenant, "--only-show-errors"); err != nil {
		return azError("grant AllDatabasesViewer on cluster "+cluster, err, stderr)
	}
	logging.FromContext(ctx).Info("granted ADX cluster viewer",
		"cluster", cluster, "resourceGroup", rg, "appId", appID, "assignment", assignmentName)
	return nil
}

// ADXPrincipalAssignment identifies a Kusto cluster principal assignment. The
// ARM Name is "<cluster>/<assignmentName>"; use adxAssignmentShortName to
// extract the assignment name for delete.
type ADXPrincipalAssignment struct {
	Name string `json:"name"`
}

// ListADXClusterAssignments lists the cluster principal assignments on a Kusto
// cluster (used by --cleanup to find gcx-created grants).
func (c *CLI) ListADXClusterAssignments(ctx context.Context, rg, cluster string) ([]ADXPrincipalAssignment, error) {
	var out []ADXPrincipalAssignment
	if err := c.tool.RunJSON(ctx, &out, "kusto", "cluster-principal-assignment", "list",
		"--cluster-name", cluster, "--resource-group", rg, "--only-show-errors", "-o", "json"); err != nil {
		return nil, classifyAuthErr(err)
	}
	return out, nil
}

// DeleteADXClusterAssignment removes a cluster principal assignment, used by
// rollback and cleanup.
func (c *CLI) DeleteADXClusterAssignment(ctx context.Context, rg, cluster, assignmentName string) error {
	if _, stderr, err := c.tool.Run(ctx, "kusto", "cluster-principal-assignment", "delete",
		"--cluster-name", cluster, "--resource-group", rg,
		"--principal-assignment-name", assignmentName, "--yes", "--only-show-errors"); err != nil {
		return azError("delete ADX principal assignment "+assignmentName, err, stderr)
	}
	logging.FromContext(ctx).Info("deleted ADX cluster assignment", "cluster", cluster, "assignment", assignmentName)
	return nil
}

// adxAssignmentShortName extracts the assignment name from an ARM child name of
// the form "<cluster>/<assignmentName>".
func adxAssignmentShortName(armName string) string {
	if i := strings.LastIndex(armName, "/"); i >= 0 {
		return armName[i+1:]
	}
	return armName
}

// DeleteAppRegistration removes an app registration (and its service principal),
// used by rollback and cleanup.
func (c *CLI) DeleteAppRegistration(ctx context.Context, appID string) error {
	if _, stderr, err := c.tool.Run(ctx, "ad", "app", "delete", "--id", appID, "--only-show-errors"); err != nil {
		return fmt.Errorf("failed to delete app registration %q: %w: %s", appID, err, strings.TrimSpace(string(stderr)))
	}
	logging.FromContext(ctx).Info("deleted app registration", "appId", appID)
	return nil
}

// authMarkers identify authorization failures in `az` error output.
var authMarkers = []string{ //nolint:gochecknoglobals
	"authorizationfailed",
	"insufficient privileges",
	"does not have authorization",
	"forbidden",
	"403",
	"authorization_requestdenied",
	"privilege",
}

func isAuthErr(text string) bool {
	lower := strings.ToLower(text)
	for _, m := range authMarkers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}

// classifyAuthErr classifies a RunJSON error (which already embeds the az
// stderr in its message) and wraps ErrInsufficientPrivilege when it looks like
// an authorization failure.
func classifyAuthErr(err error) error {
	if err == nil {
		return nil
	}
	if isAuthErr(err.Error()) {
		return fmt.Errorf("%w: %w", ErrInsufficientPrivilege, err)
	}
	return err
}

// azError builds a descriptive error from a Run-based `az` failure. It ALWAYS
// includes the captured stderr so users see the underlying `az` message instead
// of a bare "exit status 1", and classifies authorization failures as
// ErrInsufficientPrivilege so the command layer can surface actionable guidance.
func azError(desc string, err error, stderr []byte) error {
	detail := strings.TrimSpace(string(stderr))
	if isAuthErr(err.Error() + " " + detail) {
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf("%s: %w: %s", desc, ErrInsufficientPrivilege, detail)
	}
	if detail != "" {
		return fmt.Errorf("%s: %w: %s", desc, err, detail)
	}
	return fmt.Errorf("%s: %w", desc, err)
}
