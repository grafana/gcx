package permissions

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	// APIVersion is the API version for Permissions resources.
	APIVersion = "permissions.ext.grafana.app/v1alpha1"
	// FolderKind is the kind for FolderPermissions resources.
	FolderKind = "FolderPermissions"
	// DashboardKind is the kind for DashboardPermissions resources.
	DashboardKind = "DashboardPermissions"
)

//nolint:gochecknoglobals // Static descriptors used in self-registration pattern.
var (
	staticFolderDescriptor = resources.Descriptor{
		GroupVersion: schema.GroupVersion{
			Group:   "permissions.ext.grafana.app",
			Version: "v1alpha1",
		},
		Kind:     FolderKind,
		Singular: "folderpermissions",
		Plural:   "folderpermissions",
	}

	staticDashboardDescriptor = resources.Descriptor{
		GroupVersion: schema.GroupVersion{
			Group:   "permissions.ext.grafana.app",
			Version: "v1alpha1",
		},
		Kind:     DashboardKind,
		Singular: "dashboardpermissions",
		Plural:   "dashboardpermissions",
	}
)

// StaticFolderDescriptor returns the resource descriptor for FolderPermissions.
func StaticFolderDescriptor() resources.Descriptor { return staticFolderDescriptor }

// StaticDashboardDescriptor returns the resource descriptor for DashboardPermissions.
func StaticDashboardDescriptor() resources.Descriptor { return staticDashboardDescriptor }

// FolderPermissionsSchema returns a JSON Schema for the FolderPermissions resource type.
func FolderPermissionsSchema() json.RawMessage {
	return adapter.SchemaFromType[FolderPermissions](staticFolderDescriptor)
}

// DashboardPermissionsSchema returns a JSON Schema for the DashboardPermissions resource type.
func DashboardPermissionsSchema() json.RawMessage {
	return adapter.SchemaFromType[DashboardPermissions](staticDashboardDescriptor)
}

// FolderPermissionsExample returns an example FolderPermissions manifest as JSON.
func FolderPermissionsExample() json.RawMessage {
	example := map[string]any{
		"apiVersion": APIVersion,
		"kind":       FolderKind,
		"metadata": map[string]any{
			"name": "folder-abc",
		},
		"spec": map[string]any{
			"items": []map[string]any{
				{"role": "Viewer", "permission": PermissionView},
				{"role": "Editor", "permission": PermissionEdit},
				{"userLogin": "admin", "permission": PermissionAdmin},
			},
		},
	}
	b, err := json.Marshal(example)
	if err != nil {
		panic(fmt.Sprintf("permissions: failed to marshal folder example: %v", err))
	}
	return b
}

// DashboardPermissionsExample returns an example DashboardPermissions manifest as JSON.
func DashboardPermissionsExample() json.RawMessage {
	example := map[string]any{
		"apiVersion": APIVersion,
		"kind":       DashboardKind,
		"metadata": map[string]any{
			"name": "dashboard-xyz",
		},
		"spec": map[string]any{
			"items": []map[string]any{
				{"role": "Viewer", "permission": PermissionView},
				{"teamId": 7, "permission": PermissionEdit},
			},
		},
	}
	b, err := json.Marshal(example)
	if err != nil {
		panic(fmt.Sprintf("permissions: failed to marshal dashboard example: %v", err))
	}
	return b
}

// NewFolderTypedCRUD builds a TypedCRUD[FolderPermissions] for the active context.
// Only GetFn and UpdateFn are populated — permissions are a per-parent
// attribute, not a standalone resource, so List/Create/Delete are unsupported.
//
//nolint:dupl // near-duplicate of NewDashboardTypedCRUD; kept readable as two focused constructors.
func NewFolderTypedCRUD(ctx context.Context, loader GrafanaConfigLoader) (*adapter.TypedCRUD[FolderPermissions], error) {
	client, err := newClientFromLoader(ctx, loader)
	if err != nil {
		return nil, err
	}

	cfg, err := loader.LoadGrafanaConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load REST config for folder permissions: %w", err)
	}

	return &adapter.TypedCRUD[FolderPermissions]{
		GetFn: func(ctx context.Context, name string) (*FolderPermissions, error) {
			items, err := client.GetFolder(ctx, name)
			if err != nil {
				return nil, err
			}
			return &FolderPermissions{UID: name, Items: items}, nil
		},
		UpdateFn: func(ctx context.Context, name string, spec *FolderPermissions) (*FolderPermissions, error) {
			if err := client.SetFolder(ctx, name, spec.Items); err != nil {
				return nil, err
			}
			items, err := client.GetFolder(ctx, name)
			if err != nil {
				return nil, fmt.Errorf("failed to re-read folder permissions after update: %w", err)
			}
			return &FolderPermissions{UID: name, Items: items}, nil
		},
		Namespace:  cfg.Namespace,
		Descriptor: staticFolderDescriptor,
	}, nil
}

// NewDashboardTypedCRUD builds a TypedCRUD[DashboardPermissions] for the active context.
// Only GetFn and UpdateFn are populated — permissions are a per-parent
// attribute, not a standalone resource, so List/Create/Delete are unsupported.
//
//nolint:dupl // near-duplicate of NewFolderTypedCRUD; kept readable as two focused constructors.
func NewDashboardTypedCRUD(ctx context.Context, loader GrafanaConfigLoader) (*adapter.TypedCRUD[DashboardPermissions], error) {
	client, err := newClientFromLoader(ctx, loader)
	if err != nil {
		return nil, err
	}

	cfg, err := loader.LoadGrafanaConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load REST config for dashboard permissions: %w", err)
	}

	return &adapter.TypedCRUD[DashboardPermissions]{
		GetFn: func(ctx context.Context, name string) (*DashboardPermissions, error) {
			items, err := client.GetDashboard(ctx, name)
			if err != nil {
				return nil, err
			}
			return &DashboardPermissions{UID: name, Items: items}, nil
		},
		UpdateFn: func(ctx context.Context, name string, spec *DashboardPermissions) (*DashboardPermissions, error) {
			if err := client.SetDashboard(ctx, name, spec.Items); err != nil {
				return nil, err
			}
			items, err := client.GetDashboard(ctx, name)
			if err != nil {
				return nil, fmt.Errorf("failed to re-read dashboard permissions after update: %w", err)
			}
			return &DashboardPermissions{UID: name, Items: items}, nil
		},
		Namespace:  cfg.Namespace,
		Descriptor: staticDashboardDescriptor,
	}, nil
}

// NewFolderAdapterFactory returns a lazy adapter.Factory for FolderPermissions.
func NewFolderAdapterFactory(loader GrafanaConfigLoader) adapter.Factory {
	return func(ctx context.Context) (adapter.ResourceAdapter, error) {
		crud, err := NewFolderTypedCRUD(ctx, loader)
		if err != nil {
			return nil, err
		}
		return crud.AsAdapter(), nil
	}
}

// NewDashboardAdapterFactory returns a lazy adapter.Factory for DashboardPermissions.
func NewDashboardAdapterFactory(loader GrafanaConfigLoader) adapter.Factory {
	return func(ctx context.Context) (adapter.ResourceAdapter, error) {
		crud, err := NewDashboardTypedCRUD(ctx, loader)
		if err != nil {
			return nil, err
		}
		return crud.AsAdapter(), nil
	}
}
