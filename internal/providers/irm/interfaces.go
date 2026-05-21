package irm

import (
	"context"
	"encoding/json"
)

// RichAlertGroupReader is the read-side interface for the rich alert-group
// surface. It captures the methods used by the bulk-action and list/get paths
// so they can be tested against a mock without a live OnCall backend.
//
// All four methods are satisfied by *OnCallClient; the interface is defined
// here (in the irm package) rather than in oncalltypes because alertGroupAPI
// and alertGroupListFilters are package-private to irm.
type RichAlertGroupReader interface {
	// ListAlertGroupsRaw fetches paginated alert groups from the internal API
	// and returns the per-item raw JSON plus a serverHasMore flag indicating
	// whether the server reported additional pages when we stopped early due
	// to the caller-supplied cap.
	ListAlertGroupsRaw(ctx context.Context, filters alertGroupListFilters, limit int) ([]json.RawMessage, bool, error)

	// GetAlertGroupRich fetches a single alert group by ID and returns the
	// rich shape. Identity fields (PK, StartedAt) are on rich.Metadata;
	// Labels for envelope assembly remain on the raw alertGroupAPI and are
	// accessed by callers that hold a reference to the API struct.
	GetAlertGroupRich(ctx context.Context, id string) (*AlertGroupRich, error)

	// ListAlertIDs lists alert IDs (with optional cap) for an alert group.
	// Returns IDs in API order, the total count, and any error.
	ListAlertIDs(ctx context.Context, alertGroupID string, limit int) ([]string, int, error)

	// GetAlertRich fetches a single alert by ID and returns the raw API
	// response alongside the rich shape.
	GetAlertRich(ctx context.Context, id string) (*alertAPI, *AlertRich, error)

	// ResolveTeams returns a map[teamID]→teamName, fetching the OnCall teams
	// list at most once per client lifetime.
	ResolveTeams(ctx context.Context) (map[string]string, error)
}

// ListAlertGroupsRaw is the exported method wrapper that delegates to the
// package-level listAlertGroupsRaw free function, satisfying RichAlertGroupReader.
func (c *OnCallClient) ListAlertGroupsRaw(ctx context.Context, filters alertGroupListFilters, limit int) ([]json.RawMessage, bool, error) {
	return listAlertGroupsRaw(ctx, c, filters, limit)
}

// ListAlertIDs is the exported method wrapper that delegates to the
// unexported listAlertIDs receiver, satisfying RichAlertGroupReader.
func (c *OnCallClient) ListAlertIDs(ctx context.Context, alertGroupID string, limit int) ([]string, int, error) {
	return c.listAlertIDs(ctx, alertGroupID, limit)
}

// ResolveTeams is the exported method wrapper that delegates to the
// unexported resolveTeams receiver, satisfying RichAlertGroupReader.
func (c *OnCallClient) ResolveTeams(ctx context.Context) (map[string]string, error) {
	return c.resolveTeams(ctx)
}

// Compile-time assertion: *OnCallClient must implement RichAlertGroupReader.
var _ RichAlertGroupReader = (*OnCallClient)(nil)
