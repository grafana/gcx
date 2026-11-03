package org

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/httputils"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"golang.org/x/sync/errgroup"
)

// User is a membership in the selected Grafana organization.
type User struct {
	AuthLabels         []string `json:"authLabels" yaml:"authLabels"`
	AvatarURL          string   `json:"avatarUrl" yaml:"avatarUrl"`
	Created            string   `json:"created" yaml:"created"`
	IsProvisioned      bool     `json:"isProvisioned" yaml:"isProvisioned"`
	UID                string   `json:"uid" yaml:"uid"`
	OrgID              int64    `json:"orgId" yaml:"orgId"`
	UserID             int64    `json:"userId" yaml:"userId"`
	Email              string   `json:"email" yaml:"email"`
	Name               string   `json:"name" yaml:"name"`
	Login              string   `json:"login" yaml:"login"`
	Role               string   `json:"role" yaml:"role"`
	LastSeenAt         string   `json:"lastSeenAt,omitempty" yaml:"lastSeenAt,omitempty"`
	LastSeenAtAge      string   `json:"lastSeenAtAge,omitempty" yaml:"lastSeenAtAge,omitempty"`
	IsDisabled         bool     `json:"isDisabled" yaml:"isDisabled"`
	IsExternallySynced bool     `json:"isExternallySynced" yaml:"isExternallySynced"`
}

type client struct{ deps adapter.ClientDeps }

func (c *client) request(ctx context.Context, method, path string, output any) error {
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.deps.BaseURL, "/")+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.deps.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("organization user request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return providers.HandleErrorResponse(resp)
	}
	if output == nil {
		return nil
	}
	body, err := httputils.ReadResponseBody(resp.Body, httputils.DefaultResponseLimit)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, output); err != nil {
		return fmt.Errorf("decode organization users: %w", err)
	}
	return nil
}

func (c *client) list(ctx context.Context) ([]User, error) {
	users := []User{}
	if err := c.request(ctx, http.MethodGet, "/api/org/users", &users); err != nil {
		return nil, err
	}
	if users == nil {
		users = []User{}
	}
	return users, nil
}

// resolve checks all identity fields together. Numeric logins must not hide an ID collision.
func resolve(users []User, subject string) (User, error) {
	var found User
	matches := 0
	for _, user := range users {
		if subject == strconv.FormatInt(user.UserID, 10) || subject == user.Login || subject == user.Email {
			found = user
			matches++
		}
	}
	if matches == 0 {
		return User{}, fmt.Errorf("organization user %q not found in the selected organization", subject)
	}
	if matches != 1 {
		return User{}, fmt.Errorf("organization user %q is ambiguous in the selected organization: use a unique ID", subject)
	}
	if found.UserID <= 0 {
		return User{}, fmt.Errorf("organization user %q has an invalid user ID", subject)
	}
	return found, nil
}

func (c *client) delete(ctx context.Context, user User) error {
	return c.request(ctx, http.MethodDelete, "/api/org/users/"+strconv.FormatInt(user.UserID, 10), nil)
}

// remove attempts every target with bounded concurrency and keeps input order for failures.
func (c *client) remove(ctx context.Context, targets []User) cmdio.BatchMutation {
	result := cmdio.NewBatchMutation("deleted")
	failures := make([]error, len(targets))
	var group errgroup.Group
	group.SetLimit(10)
	for i, user := range targets {
		group.Go(func() error {
			err := c.delete(ctx, user)
			failures[i] = err
			return nil
		})
	}
	_ = group.Wait()
	for i, err := range failures {
		if err != nil {
			result.Summary.Failed++
			result.Failures = append(result.Failures, cmdio.MutationFailure{Target: mutationTarget(targets[i]), Error: err.Error()})
		} else {
			result.Summary.Succeeded++
		}
	}
	return result
}
