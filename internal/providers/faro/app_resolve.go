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

// prepareUpdate checks app against the stored app and fills in what the
// manifest leaves out. The API ignores a new name on update, so any change to
// spec.name, even in case or punctuation, is an error rather than a silent
// no-op. The API replaces corsOrigins and extraLogLabels in full, so an
// omitted list keeps the stored one and only an explicit empty list clears it.
func prepareUpdate(current, app *FaroApp) error {
	if app.Name != current.Name {
		return fmt.Errorf("faro app %s is named %q, not %q, and the API cannot rename an app: set spec.name to %q to update it, or create a new app with \"gcx frontend apps create\" or with \"gcx resources push\" and no metadata.name",
			current.GetResourceName(), current.Name, app.Name, current.Name)
	}
	if app.CORSOrigins == nil {
		app.CORSOrigins = current.CORSOrigins
	}
	if app.ExtraLogLabels == nil {
		app.ExtraLogLabels = current.ExtraLogLabels
	}
	return nil
}

// resolveApp finds the app that a command argument names: a display name, a
// slug-id (my-web-app-42) or a numeric ID.
func resolveApp(ctx context.Context, crud *adapter.TypedCRUD[FaroApp], arg string) (*adapter.TypedObject[FaroApp], error) {
	apps, err := crud.List(ctx, 0)
	if err != nil {
		return nil, err
	}
	return findApp(apps, arg)
}

// findApp returns the app in apps that arg names. A slug-id counts only when
// its slug is that app's own, so a display name that ends in "-<digits>" never
// resolves to the app with that ID. An argument that is one app's name and
// another app's slug-id or ID is an error.
func findApp(apps []adapter.TypedObject[FaroApp], arg string) (*adapter.TypedObject[FaroApp], error) {
	byName, byID := -1, -1
	for i := range apps {
		app := &apps[i].Spec
		if app.Name == arg {
			byName = i
		}
		if app.ID == arg || strings.EqualFold(app.GetResourceName(), arg) {
			byID = i
		}
	}
	switch {
	case byName >= 0 && byID >= 0 && byName != byID:
		return nil, fmt.Errorf("faro app %q is ambiguous: it is the name of %s and the slug-id or ID of %s; pass the slug-id of the app you mean",
			arg, apps[byName].Spec.GetResourceName(), apps[byID].Spec.GetResourceName())
	case byName >= 0:
		return &apps[byName], nil
	case byID >= 0:
		return &apps[byID], nil
	}
	return nil, fmt.Errorf("faro app %q: no app has that slug-id or name: %w", arg, adapter.ErrNotFound)
}
