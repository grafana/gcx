package faro

import (
	"context"
	"fmt"
	"strings"

	"github.com/grafana/gcx/internal/resources/adapter"
)

// newAppCRUD builds the TypedCRUD[FaroApp] that both the frontend commands and
// the resources adapter use, so resource names resolve the same way in both.
func newAppCRUD(client *Client, namespace string) *adapter.TypedCRUD[FaroApp] {
	return &adapter.TypedCRUD[FaroApp]{
		ListFn: adapter.LimitedListFn(client.List),

		GetFn: func(ctx context.Context, name string) (*FaroApp, error) {
			return lookupByResourceName(ctx, client, name)
		},

		CreateFn: func(ctx context.Context, app *FaroApp) (*FaroApp, error) {
			return client.Create(ctx, app)
		},

		UpdateFn: func(ctx context.Context, name string, app *FaroApp) (*FaroApp, error) {
			current, err := lookupByResourceName(ctx, client, name)
			if err != nil {
				return nil, err
			}
			if err := prepareUpdate(current, app); err != nil {
				return nil, err
			}
			return client.Update(ctx, current.ID, app)
		},

		DeleteFn: func(ctx context.Context, name string) error {
			current, err := lookupByResourceName(ctx, client, name)
			if err != nil {
				return err
			}
			return client.Delete(ctx, current.ID)
		},

		StripFields: []string{"id"},
		Namespace:   namespace,
		Descriptor:  staticDescriptor,
	}
}

// lookupByResourceName fetches the app that a resource name such as
// "my-web-app-42" refers to. The slug must be the app's own, so "checkout-2"
// does not refer to app 2 when app 2 is named "web". A bare numeric ID is used
// as-is.
func lookupByResourceName(ctx context.Context, client *Client, name string) (*FaroApp, error) {
	id, ok := adapter.ExtractIDFromSlug(name)
	if !ok {
		return nil, fmt.Errorf("faro app %q: %w", name, adapter.ErrNotFound)
	}
	app, err := client.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if name != id && !strings.EqualFold(name, app.GetResourceName()) {
		return nil, fmt.Errorf("faro app %q: app %s is %q: %w", name, id, app.GetResourceName(), adapter.ErrNotFound)
	}
	return app, nil
}

// prepareUpdate checks app against the stored app. The API never renames an
// app, so a different spec.name means the manifest names another app.
func prepareUpdate(current, app *FaroApp) error {
	// Compare slugs, as the natural key does, so a cross-stack push of
	// "My App" still updates an app stored as "my-app".
	if adapter.SlugifyName(app.Name) != adapter.SlugifyName(current.Name) {
		return fmt.Errorf("faro app %s is named %q, not %q, and the API cannot rename an app: set spec.name to %q, or remove the ID from metadata.name to create a new app",
			current.GetResourceName(), current.Name, app.Name, current.Name)
	}
	app.Name = current.Name
	return nil
}

// resolveApp finds the app that a command argument names: a display name, a
// slug-id (my-web-app-42) or a numeric ID.
func resolveApp(ctx context.Context, crud *adapter.TypedCRUD[FaroApp], arg string) (*adapter.TypedObject[FaroApp], error) {
	apps, err := crud.List(ctx, 0)
	if err != nil {
		return nil, err
	}
	specs := make([]FaroApp, len(apps))
	for i := range apps {
		specs[i] = apps[i].Spec
	}
	i, err := findApp(specs, arg)
	if err != nil {
		return nil, err
	}
	return &apps[i], nil
}

// findApp returns the index of the app that arg names. A slug-id counts only
// when its slug is that app's own, so a display name that ends in "-<digits>"
// never resolves to the app with that ID. An argument that is one app's name
// and another app's slug-id or ID is an error.
func findApp(apps []FaroApp, arg string) (int, error) {
	byName, byID := -1, -1
	for i, app := range apps {
		if app.Name == arg {
			byName = i
		}
		if app.ID == arg || strings.EqualFold(app.GetResourceName(), arg) {
			byID = i
		}
	}
	switch {
	case byName >= 0 && byID >= 0 && byName != byID:
		return -1, fmt.Errorf("faro app %q is ambiguous: it is the name of %s and the slug-id or ID of %s; pass the slug-id of the app you mean",
			arg, apps[byName].GetResourceName(), apps[byID].GetResourceName())
	case byName >= 0:
		return byName, nil
	case byID >= 0:
		return byID, nil
	}
	return -1, fmt.Errorf("faro app %q: no app has that slug-id or name: %w", arg, adapter.ErrNotFound)
}
