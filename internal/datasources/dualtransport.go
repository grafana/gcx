package datasources

import (
	"context"
	"errors"
)

// dualTransport prefers the app-platform datasource API and transparently falls
// back to the legacy REST API per call when app-platform is not served for that
// operation. Fallback is keyed on the errK8sNotServed sentinel; any other error
// (including a genuine 404 for a served resource) propagates unchanged so
// IsNotFound semantics are preserved.
type dualTransport struct {
	rest *Client
	k8s  *k8sTransport
}

var _ Transport = (*dualTransport)(nil)

// k8sThenREST runs the app-platform attempt and falls back to REST only when the
// app-platform surface reported "not served".
func k8sThenREST[T any](k8sFn, restFn func() (T, error)) (T, error) {
	v, err := k8sFn()
	if errors.Is(err, errK8sNotServed) {
		return restFn()
	}
	return v, err
}

func (d *dualTransport) List(ctx context.Context) ([]*Datasource, error) {
	return k8sThenREST(
		func() ([]*Datasource, error) { return d.k8s.List(ctx) },
		func() ([]*Datasource, error) { return d.rest.List(ctx) },
	)
}

func (d *dualTransport) GetByUID(ctx context.Context, uid string) (*Datasource, error) {
	return k8sThenREST(
		func() (*Datasource, error) { return d.k8s.GetByUID(ctx, uid) },
		func() (*Datasource, error) { return d.rest.GetByUID(ctx, uid) },
	)
}

func (d *dualTransport) Create(ctx context.Context, ds *Datasource) (*Datasource, error) {
	return k8sThenREST(
		func() (*Datasource, error) { return d.k8s.Create(ctx, ds) },
		func() (*Datasource, error) { return d.rest.Create(ctx, ds) },
	)
}

func (d *dualTransport) Update(ctx context.Context, uid string, ds *Datasource) (*Datasource, error) {
	return k8sThenREST(
		func() (*Datasource, error) { return d.k8s.Update(ctx, uid, ds) },
		func() (*Datasource, error) { return d.rest.Update(ctx, uid, ds) },
	)
}

func (d *dualTransport) Delete(ctx context.Context, uid string) error {
	_, err := k8sThenREST(
		func() (struct{}, error) { return struct{}{}, d.k8s.Delete(ctx, uid) },
		func() (struct{}, error) { return struct{}{}, d.rest.Delete(ctx, uid) },
	)
	return err
}

func (d *dualTransport) Health(ctx context.Context, uid string) (*HealthResult, error) {
	return k8sThenREST(
		func() (*HealthResult, error) { return d.k8s.Health(ctx, uid) },
		func() (*HealthResult, error) { return d.rest.Health(ctx, uid) },
	)
}
