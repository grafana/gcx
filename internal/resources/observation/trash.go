package observation

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// TypeDashboardTrash is the observation key for soft-deleted dashboards. It is
// a view of the dashboards type, listed with the grafana.app/get-trash label
// selector, not a resource type of its own. In a diff, an added trash entry is
// a dashboard that was deleted, and a deleted trash entry is one that left the
// trash: restored, purged, or replaced by a new dashboard with the same name.
const TypeDashboardTrash = "trash.dashboards.dashboard.grafana.app"

const trashLabelSelector = "grafana.app/get-trash=true"

// trashPageLimit bounds each trash list page; pages are followed with the
// continue token until the list is exhausted.
const trashPageLimit = 500

// maxTrashResponse bounds one trash list page.
const maxTrashResponse = 64 << 20

// TrashReader lists the dashboard trash at the given dashboards API version.
type TrashReader func(ctx context.Context, version string) (TypeCoverage, map[string]map[string]any, error)

// NewTrashReader returns a TrashReader that lists
// /apis/dashboard.grafana.app/<version>/namespaces/<namespace>/dashboards with
// the trash label selector. A 404 means the stack does not serve a trash, and
// the view is recorded as unavailable. Any other failure, and any list that
// cannot be followed to its end, makes the view incomplete. The error is set
// only when ctx is done.
func NewTrashReader(client *http.Client, host, namespace string) TrashReader {
	host = strings.TrimRight(host, "/")
	return func(ctx context.Context, version string) (TypeCoverage, map[string]map[string]any, error) {
		cov := TypeCoverage{Type: TypeDashboardTrash, Selector: TypeDashboardTrash, Version: version, Kind: "Dashboard"}
		objs := map[string]map[string]any{}
		var reasons []string
		cont := ""
		for {
			q := url.Values{}
			q.Set("labelSelector", trashLabelSelector)
			q.Set("limit", strconv.Itoa(trashPageLimit))
			if cont != "" {
				q.Set("continue", cont)
			}
			u := fmt.Sprintf("%s/apis/dashboard.grafana.app/%s/namespaces/%s/dashboards?%s", host, version, namespace, q.Encode())
			page, status, err := getTrashPage(ctx, client, u)
			if ctx.Err() != nil {
				return cov, nil, ctx.Err()
			}
			if status == http.StatusNotFound {
				cov.Status = CoverageUnavailable
				cov.Reasons = []string{"the stack does not serve a dashboard trash"}
				return cov, nil, nil
			}
			if err != nil {
				reasons = append(reasons, "read failed: "+err.Error())
				objs = nil
				break
			}
			for _, item := range page.Items {
				meta, _ := item["metadata"].(map[string]any)
				name, _ := meta["name"].(string)
				if name == "" {
					reasons = append(reasons, "trash entry without a name")
					continue
				}
				if _, dup := objs[name]; dup {
					reasons = append(reasons, "duplicate trash entry: "+name)
					continue
				}
				norm, nerr := Normalize(TypeDashboardTrash, item)
				if nerr != nil {
					reasons = append(reasons, "normalize failed: "+name+": "+nerr.Error())
					continue
				}
				objs[name] = norm
			}
			cont = page.Metadata.Continue
			if cont == "" {
				break
			}
		}
		cov.Count = len(objs)
		if len(reasons) == 0 {
			cov.Status = CoverageComplete
		} else {
			sort.Strings(reasons)
			cov.Status = CoverageIncomplete
			cov.Reasons = reasons
		}
		return cov, objs, nil
	}
}

type trashPage struct {
	Metadata struct {
		Continue string `json:"continue"`
	} `json:"metadata"`
	Items []map[string]any `json:"items"`
}

func getTrashPage(ctx context.Context, client *http.Client, u string) (trashPage, int, error) {
	var page trashPage
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return page, 0, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return page, 0, err
	}
	defer resp.Body.Close()
	body := io.LimitReader(resp.Body, maxTrashResponse)
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(body, 512))
		return page, resp.StatusCode, &StatusError{Path: req.URL.Path, Status: resp.StatusCode, Body: strings.TrimSpace(string(msg))}
	}
	if err := json.NewDecoder(body).Decode(&page); err != nil {
		return page, resp.StatusCode, fmt.Errorf("decode trash list: %w", err)
	}
	return page, resp.StatusCode, nil
}
