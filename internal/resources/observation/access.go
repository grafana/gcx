package observation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/grafana/gcx/internal/config"
	"k8s.io/client-go/rest"
)

// Grafana filters list results by the caller's permissions without an error.
// A caller that cannot read a resource never sees it, so a capture taken with
// restricted access looks complete while it omits resources. A diff between
// captures taken by different callers then reports the difference in access
// as additions or deletions. The capture therefore records who took it and
// whether that caller can read every resource of each type.

// Read scope values for one type in a capture.
const (
	// ReadScopeAll means the caller holds read access to every resource of
	// the type.
	ReadScopeAll = "all"
	// ReadScopeRestricted means the caller can read only some resources of
	// the type.
	ReadScopeRestricted = "restricted"
	// ReadScopeUnknown means the caller's permissions could not be read.
	ReadScopeUnknown = "unknown"
)

// CallerIdentity identifies the Grafana user or service account that took a
// capture. Captures taken by different callers are not comparable.
type CallerIdentity struct {
	// UID is the Grafana identity UID, for example service-account:2.
	UID string `json:"uid"`
	// Login is informational and is not part of the comparison.
	Login string `json:"login,omitempty"`
}

// Access is what a capture knows about its caller.
type Access struct {
	Identity CallerIdentity
	// Permissions maps actions to the scopes the caller holds, as Grafana
	// reports them. It is nil when the permissions could not be read.
	Permissions map[string][]string
	// PermissionsErr explains why Permissions is nil.
	PermissionsErr error
}

// readPermissions are, per type, the read action and the scopes that grant it
// on every resource of the type.
//
//nolint:gochecknoglobals // constant-like lookup table.
var readPermissions = map[string]struct {
	action string
	scopes []string
}{
	TypeFolders:     {"folders:read", []string{"*", "folders:*", "folders:uid:*"}},
	TypeDashboards:  {"dashboards:read", []string{"*", "dashboards:*", "dashboards:uid:*", "folders:*", "folders:uid:*"}},
	TypeDatasources: {"datasources:read", []string{"*", "datasources:*", "datasources:uid:*"}},
	TypeDashboardPermissions: {"dashboards.permissions:read",
		[]string{"*", "dashboards:*", "dashboards:uid:*", "folders:*", "folders:uid:*"}},
	TypeFolderPermissions: {"folders.permissions:read", []string{"*", "folders:*", "folders:uid:*"}},
}

// ReadScope reports whether the caller can read every resource of a type.
// The reason is set when it cannot, or when that cannot be established.
func (a Access) ReadScope(typeKey string) (string, string) {
	perm, ok := readPermissions[typeKey]
	if !ok {
		return ReadScopeUnknown, "no read permission is known for this type"
	}
	if a.Permissions == nil {
		reason := "the caller's permissions could not be read"
		if a.PermissionsErr != nil {
			reason += ": " + a.PermissionsErr.Error()
		}
		return ReadScopeUnknown, reason
	}
	for _, scope := range a.Permissions[perm.action] {
		if slices.Contains(perm.scopes, scope) {
			return ReadScopeAll, ""
		}
	}
	// The message leaves out the global "*" (scopes[0]) on purpose: the
	// type's own wildcard scopes are the grant to request.
	return ReadScopeRestricted, fmt.Sprintf("the caller cannot read every resource: %s is not granted on %s", perm.action, strings.Join(perm.scopes[1:], " or "))
}

// maxAccessResponse bounds the identity and permission responses.
const maxAccessResponse = 8 << 20

// ReadAccess reads the caller's identity from /api/user and permissions from
// /api/access-control/user/permissions. An identity error fails the call,
// because a capture without an identity cannot be compared. A permissions
// error is recorded in Access, so that each type is reported with an unknown
// read scope.
func ReadAccess(ctx context.Context, cfg config.NamespacedRESTConfig) (Access, error) {
	client, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return Access{}, fmt.Errorf("create HTTP client: %w", err)
	}
	host := strings.TrimRight(cfg.Host, "/")

	var user struct {
		UID   string `json:"uid"`
		Login string `json:"login"`
	}
	if err := getJSON(ctx, client, host+"/api/user", &user); err != nil {
		return Access{}, fmt.Errorf("read the caller identity: %w", err)
	}
	if user.UID == "" {
		return Access{}, errors.New("read the caller identity: /api/user returned no uid")
	}

	access := Access{Identity: CallerIdentity{UID: user.UID, Login: user.Login}}
	var perms map[string][]string
	if err := getJSON(ctx, client, host+"/api/access-control/user/permissions", &perms); err != nil {
		access.PermissionsErr = err
	} else {
		access.Permissions = perms
	}
	return access, nil
}

// StatusError is a non-200 response from an identity or permissions request.
type StatusError struct {
	Path   string
	Status int
	Body   string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("%s returned status %d: %s", e.Path, e.Status, e.Body)
}

func getJSON(ctx context.Context, client *http.Client, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body := io.LimitReader(resp.Body, maxAccessResponse)
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(body, 512))
		return &StatusError{Path: req.URL.Path, Status: resp.StatusCode, Body: strings.TrimSpace(string(msg))}
	}
	if err := json.NewDecoder(body).Decode(out); err != nil {
		return fmt.Errorf("decode %s: %w", req.URL.Path, err)
	}
	return nil
}
