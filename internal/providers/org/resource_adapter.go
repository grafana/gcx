package org

import (
	"context"
	"encoding/json"
	"fmt"

	internalconfig "github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	// APIVersion is the API version for org user resources.
	APIVersion = "org.ext.grafana.app/v1alpha1"
	// Kind is the kind for org user resources.
	Kind = "OrgUser"
)

// staticUsersDescriptor is the resource descriptor for org user resources.
//
//nolint:gochecknoglobals // Static descriptor used in self-registration pattern.
var staticUsersDescriptor = resources.Descriptor{
	GroupVersion: schema.GroupVersion{
		Group:   "org.ext.grafana.app",
		Version: "v1alpha1",
	},
	Kind:     Kind,
	Singular: "user",
	Plural:   "users",
}

// StaticUsersDescriptor returns the static descriptor for org user resources.
func StaticUsersDescriptor() resources.Descriptor {
	return staticUsersDescriptor
}

// OrgUserSchema returns a JSON Schema for the OrgUser resource type.
func OrgUserSchema() json.RawMessage {
	return adapter.SchemaFromType[OrgUser](StaticUsersDescriptor())
}

// OrgUserExample returns an example OrgUser manifest as JSON.
func OrgUserExample() json.RawMessage {
	example := map[string]any{
		"apiVersion": APIVersion,
		"kind":       Kind,
		"metadata": map[string]any{
			"name": "42",
		},
		"spec": map[string]any{
			"login": "alice",
			"email": "alice@example.com",
			"role":  "Editor",
		},
	}
	b, err := json.Marshal(example)
	if err != nil {
		panic(fmt.Sprintf("org: failed to marshal example: %v", err))
	}
	return b
}

// NewUsersAdapterFactory returns a lazy adapter.Factory for org users.
// The factory captures the loader and constructs the client on first invocation.
func NewUsersAdapterFactory(loader GrafanaConfigLoader) adapter.Factory {
	return func(ctx context.Context) (adapter.ResourceAdapter, error) {
		crud, _, err := NewUsersTypedCRUD(ctx, loader)
		if err != nil {
			return nil, err
		}
		return crud.AsAdapter(), nil
	}
}

// NewUsersTypedCRUD creates a TypedCRUD[OrgUser] for the org users resource,
// wiring the org API client into typed CRUD operations. It is exported so CLI
// commands can route through the same pipeline as adapter registration.
func NewUsersTypedCRUD(ctx context.Context, loader GrafanaConfigLoader) (*adapter.TypedCRUD[OrgUser], internalconfig.NamespacedRESTConfig, error) {
	cfg, err := loader.LoadGrafanaConfig(ctx)
	if err != nil {
		return nil, internalconfig.NamespacedRESTConfig{}, fmt.Errorf("failed to load REST config for org: %w", err)
	}

	client, err := NewClient(cfg)
	if err != nil {
		return nil, internalconfig.NamespacedRESTConfig{}, fmt.Errorf("failed to create org client: %w", err)
	}

	crud := &adapter.TypedCRUD[OrgUser]{
		ListFn: adapter.LimitedListFn(client.List),

		GetFn: func(ctx context.Context, name string) (*OrgUser, error) {
			id, err := parseUserID(name)
			if err != nil {
				return nil, err
			}
			return client.Get(ctx, id)
		},

		CreateFn: func(ctx context.Context, u *OrgUser) (*OrgUser, error) {
			loginOrEmail := u.LoginOrEmail
			if loginOrEmail == "" {
				loginOrEmail = u.Login
			}
			if loginOrEmail == "" {
				loginOrEmail = u.Email
			}
			if err := client.Add(ctx, AddUserRequest{
				LoginOrEmail: loginOrEmail,
				Role:         u.Role,
			}); err != nil {
				return nil, err
			}
			// POST /api/org/users returns no body with the new ID; re-fetch by
			// login/email to populate UserID for the returned spec.
			fetched, err := client.GetByLoginOrEmail(ctx, loginOrEmail)
			if err != nil {
				return nil, fmt.Errorf("fetching org user %q after add: %w", loginOrEmail, err)
			}
			return fetched, nil
		},

		UpdateFn: func(ctx context.Context, name string, u *OrgUser) (*OrgUser, error) {
			id, err := parseUserID(name)
			if err != nil {
				return nil, err
			}
			if err := client.UpdateUserRole(ctx, id, u.Role); err != nil {
				return nil, err
			}
			return client.Get(ctx, id)
		},

		DeleteFn: func(ctx context.Context, name string) error {
			id, err := parseUserID(name)
			if err != nil {
				return err
			}
			return client.RemoveUser(ctx, id)
		},

		StripFields: []string{"userId", "orgId"},
		Namespace:   cfg.Namespace,
		Descriptor:  staticUsersDescriptor,
	}

	return crud, cfg, nil
}
