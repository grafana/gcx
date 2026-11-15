package observation

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"golang.org/x/sync/errgroup"
)

// Permission views record the stored permission entries of each captured
// dashboard and folder. They are views of those types, read from the legacy
// permissions endpoints, not resource types of their own. Entries are
// normalized to principal, permission and inherited, and sorted, because the
// endpoints return them in no stable order. They record stored entries, not
// effective access: access that a dashboard gets from its folder is not
// always listed, and a change takes time to reach other principals.
const (
	TypeDashboardPermissions = "permissions.dashboards.dashboard.grafana.app"
	TypeFolderPermissions    = "permissions.folders.folder.grafana.app"
)

// maxPermissionsResponse bounds one permissions response.
const maxPermissionsResponse = 8 << 20

// PermissionReader reads the stored permission entries of the named
// resources of a parent type (TypeDashboards or TypeFolders).
type PermissionReader func(ctx context.Context, parentType string, names []string) (TypeCoverage, map[string]map[string]any, error)

// permissionViews maps a parent type to its permission view and endpoint.
//
//nolint:gochecknoglobals // constant-like lookup table.
var permissionViews = map[string]struct {
	view string
	path string
}{
	TypeDashboards: {TypeDashboardPermissions, "/api/dashboards/uid/%s/permissions"},
	TypeFolders:    {TypeFolderPermissions, "/api/folders/%s/permissions"},
}

// permissionEntry is the raw legacy permission item.
type permissionEntry struct {
	UserLogin      string `json:"userLogin"`
	TeamUID        string `json:"teamUid"`
	Team           string `json:"team"`
	Role           string `json:"role"`
	PermissionName string `json:"permissionName"`
	Inherited      bool   `json:"inherited"`
}

func (e permissionEntry) principal() string {
	switch {
	case e.UserLogin != "":
		return "user:" + e.UserLogin
	case e.TeamUID != "":
		return "team:" + e.TeamUID
	case e.Team != "":
		return "team:" + e.Team
	case e.Role != "":
		return "role:" + e.Role
	default:
		return "unknown"
	}
}

// normalizePermissions returns the observable form of one resource's entries.
func normalizePermissions(view, name string, entries []permissionEntry) map[string]any {
	norm := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		norm = append(norm, map[string]any{"principal": e.principal(), "permission": e.PermissionName, "inherited": e.Inherited})
	}
	sort.Slice(norm, func(i, j int) bool {
		return fmt.Sprint(norm[i]["principal"], norm[i]["permission"], norm[i]["inherited"]) <
			fmt.Sprint(norm[j]["principal"], norm[j]["permission"], norm[j]["inherited"])
	})
	entriesAny := make([]any, len(norm))
	for i, n := range norm {
		entriesAny[i] = n
	}
	return map[string]any{
		"apiVersion": "observation.gcx.grafana.app/v0",
		"kind":       "PermissionSet",
		"metadata":   map[string]any{"name": name},
		"spec":       map[string]any{"of": view, "entries": entriesAny},
	}
}

// NewPermissionReader reads permissions one resource at a time, so that one
// failed read costs only that resource. A resource deleted before its read
// (404) is omitted. Any other failure makes the view incomplete. The error is
// set only when ctx is done.
func NewPermissionReader(client *http.Client, host string) PermissionReader {
	host = strings.TrimRight(host, "/")
	return func(ctx context.Context, parentType string, names []string) (TypeCoverage, map[string]map[string]any, error) {
		pv := permissionViews[parentType]
		cov := TypeCoverage{Type: pv.view, Selector: pv.view, Kind: "PermissionSet"}
		results := make([]map[string]any, len(names))
		reasons := make([]string, len(names))
		g, gctx := errgroup.WithContext(ctx)
		g.SetLimit(DefaultConcurrency)
		for idx, name := range names {
			g.Go(func() error {
				var entries []permissionEntry
				status, err := getPermissions(gctx, client, host+fmt.Sprintf(pv.path, url.PathEscape(name)), &entries)
				switch {
				case gctx.Err() != nil:
					return gctx.Err()
				case status == http.StatusNotFound:
				case err != nil:
					reasons[idx] = "read failed: " + name + ": " + err.Error()
				default:
					results[idx] = normalizePermissions(pv.view, name, entries)
				}
				return nil
			})
		}
		if err := g.Wait(); err != nil {
			return cov, nil, err
		}
		objs := map[string]map[string]any{}
		var rs []string
		for idx, name := range names {
			if reasons[idx] != "" {
				rs = append(rs, reasons[idx])
			}
			if results[idx] != nil {
				objs[name] = results[idx]
			}
		}
		cov.Count = len(objs)
		if len(rs) == 0 {
			cov.Status = CoverageComplete
		} else {
			sort.Strings(rs)
			cov.Status = CoverageIncomplete
			cov.Reasons = rs
		}
		return cov, objs, nil
	}
}

func getPermissions(ctx context.Context, client *http.Client, u string, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body := io.LimitReader(resp.Body, maxPermissionsResponse)
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(body, 512))
		return resp.StatusCode, &StatusError{Path: req.URL.Path, Status: resp.StatusCode, Body: strings.TrimSpace(string(msg))}
	}
	if err := json.NewDecoder(body).Decode(out); err != nil {
		return resp.StatusCode, fmt.Errorf("decode permissions: %w", err)
	}
	return resp.StatusCode, nil
}
