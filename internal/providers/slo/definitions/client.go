package definitions

import (
	"context"
	"encoding/json"
	"fmt"

	sloclient "github.com/grafana/gcx/client/slo"
	"github.com/grafana/gcx/internal/config"
	"k8s.io/client-go/rest"
)

// ErrNotFound is returned when a requested SLO does not exist (HTTP 404).
var ErrNotFound = sloclient.ErrNotFound

// Client is a thin CLI-facing wrapper around the public client/slo.Client:
// it builds the *http.Client from a NamespacedRESTConfig (via client-go's
// rest.HTTPClientFor) and converts between the CLI's Slo type — which
// carries resource-adapter methods the public API has no business
// exposing — and the public client's wire type.
type Client struct {
	inner *sloclient.Client
}

// NewClient creates a new SLO definitions client.
func NewClient(cfg config.NamespacedRESTConfig) (*Client, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	return &Client{inner: sloclient.NewClient(httpClient, cfg.Host)}, nil
}

// List returns all SLO definitions.
func (c *Client) List(ctx context.Context) ([]Slo, error) {
	items, err := c.inner.List(ctx)
	if err != nil {
		return nil, err
	}

	slos := make([]Slo, len(items))
	for i, item := range items {
		slo, err := fromWire(&item)
		if err != nil {
			return nil, fmt.Errorf("failed to convert SLO %s: %w", item.UUID, err)
		}
		slos[i] = *slo
	}

	return slos, nil
}

// Get returns a single SLO definition by UUID.
func (c *Client) Get(ctx context.Context, uuid string) (*Slo, error) {
	item, err := c.inner.Get(ctx, uuid)
	if err != nil {
		return nil, err
	}

	return fromWire(item)
}

// Create creates a new SLO definition and returns the created object.
func (c *Client) Create(ctx context.Context, slo *Slo) (*Slo, error) {
	wire, err := toWire(slo)
	if err != nil {
		return nil, fmt.Errorf("failed to convert SLO %s: %w", slo.Name, err)
	}

	created, err := c.inner.Create(ctx, wire)
	if err != nil {
		return nil, err
	}

	return fromWire(created)
}

// Update updates an existing SLO definition and returns the updated object.
func (c *Client) Update(ctx context.Context, uuid string, slo *Slo) (*Slo, error) {
	wire, err := toWire(slo)
	if err != nil {
		return nil, fmt.Errorf("failed to convert SLO %s: %w", uuid, err)
	}

	updated, err := c.inner.Update(ctx, uuid, wire)
	if err != nil {
		return nil, err
	}

	return fromWire(updated)
}

// Delete deletes an SLO definition by UUID. confirmed is resolved by the
// caller (the CLI's ConfirmDestructive prompt, already run before this is
// reached) and passed straight through to the public client, which has no
// stdin of its own to prompt against.
func (c *Client) Delete(ctx context.Context, uuid string, confirmed bool) error {
	return c.inner.Delete(ctx, uuid, confirmed)
}

// toWire converts the CLI's Slo (which also carries resource-adapter
// methods irrelevant to the wire format) to the public client's wire type.
// Both types share identical JSON tags, so a marshal round-trip is a
// correct and simple field-for-field copy without hand-mapping every
// nested struct.
func toWire(slo *Slo) (*sloclient.Slo, error) {
	b, err := json.Marshal(slo)
	if err != nil {
		return nil, err
	}
	var wire sloclient.Slo
	if err := json.Unmarshal(b, &wire); err != nil {
		return nil, err
	}
	return &wire, nil
}

// fromWire converts the public client's wire type back to the CLI's Slo.
func fromWire(wire *sloclient.Slo) (*Slo, error) {
	b, err := json.Marshal(wire)
	if err != nil {
		return nil, err
	}
	var slo Slo
	if err := json.Unmarshal(b, &slo); err != nil {
		return nil, err
	}
	return &slo, nil
}
