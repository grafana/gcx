package org

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/httputils"
	"github.com/grafana/gcx/internal/providers"
)

// ServiceAccount is an account in the selected organization.
type ServiceAccount struct {
	ID            int64            `json:"id" yaml:"id"`
	UID           string           `json:"uid,omitempty" yaml:"uid,omitempty"`
	Name          string           `json:"name" yaml:"name"`
	Login         string           `json:"login,omitempty" yaml:"login,omitempty"`
	Role          string           `json:"role" yaml:"role"`
	OrgID         int64            `json:"orgId" yaml:"orgId"`
	IsDisabled    bool             `json:"isDisabled" yaml:"isDisabled"`
	Tokens        int64            `json:"tokens" yaml:"tokens"`
	AvatarURL     string           `json:"avatarUrl,omitempty" yaml:"avatarUrl,omitempty"`
	AccessControl map[string]bool  `json:"accessControl,omitempty" yaml:"accessControl,omitempty"`
	CreatedAt     string           `json:"createdAt,omitempty" yaml:"createdAt,omitempty"`
	UpdatedAt     string           `json:"updatedAt,omitempty" yaml:"updatedAt,omitempty"`
	Teams         []map[string]any `json:"teams,omitempty" yaml:"teams,omitempty"`
}

// Role is a Grafana RBAC role. It is not a basic organization role.
type Role struct {
	UID         string           `json:"uid" yaml:"uid"`
	Name        string           `json:"name" yaml:"name"`
	DisplayName string           `json:"displayName,omitempty" yaml:"displayName,omitempty"`
	Description string           `json:"description,omitempty" yaml:"description,omitempty"`
	Group       string           `json:"group,omitempty" yaml:"group,omitempty"`
	Version     int64            `json:"version,omitempty" yaml:"version,omitempty"`
	Global      bool             `json:"global" yaml:"global"`
	Hidden      bool             `json:"hidden" yaml:"hidden"`
	Permissions []map[string]any `json:"permissions,omitempty" yaml:"permissions,omitempty"`
}

type tokenResponse struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Key  string `json:"key"`
}

func (c *client) send(ctx context.Context, method, path string, input, output any) error {
	var body bytes.Buffer
	if input != nil {
		if err := json.NewEncoder(&body).Encode(input); err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.deps.BaseURL, "/")+path, &body)
	if err != nil {
		return err
	}
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.deps.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("organization request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return providers.HandleErrorResponse(resp)
	}
	if output == nil {
		return nil
	}
	data, err := httputils.ReadResponseBody(resp.Body, httputils.DefaultResponseLimit)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, output); err != nil {
		return fmt.Errorf("decode organization response: %w", err)
	}
	return nil
}

func (c *client) serviceAccounts(ctx context.Context) ([]ServiceAccount, error) {
	accounts := []ServiceAccount{}
	for page := 1; ; page++ {
		var response struct {
			TotalCount      int              `json:"totalCount"`
			ServiceAccounts []ServiceAccount `json:"serviceAccounts"`
		}
		path := "/api/serviceaccounts/search?perpage=100&page=" + strconv.Itoa(page)
		if err := c.send(ctx, http.MethodGet, path, nil, &response); err != nil {
			return nil, err
		}
		accounts = append(accounts, response.ServiceAccounts...)
		if len(accounts) >= response.TotalCount {
			return accounts, nil
		}
		if len(response.ServiceAccounts) == 0 {
			return nil, errors.New("service account pagination stopped before totalCount")
		}
	}
}

func (c *client) roles(ctx context.Context) ([]Role, error) {
	roles := []Role{}
	if err := c.send(ctx, http.MethodGet, "/api/access-control/roles", nil, &roles); err != nil {
		return nil, err
	}
	if roles == nil {
		roles = []Role{}
	}
	return roles, nil
}

func accountPath(id int64) string { return "/api/serviceaccounts/" + strconv.FormatInt(id, 10) }
func tokenPath(accountID, tokenID int64) string {
	return accountPath(accountID) + "/tokens/" + strconv.FormatInt(tokenID, 10)
}
