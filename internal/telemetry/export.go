package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"time"

	"github.com/grafana/gcx/internal/version"
)

// defaultEndpoint is the usage-stats receiver. GCX_TELEMETRY_ENDPOINT
// overrides it, for pointing test builds at a dev deployment.
const defaultEndpoint = "https://stats.grafana.org/gcx-usage-report"

// exportTimeout caps the whole export request. Telemetry must never
// noticeably delay CLI exit, so this is deliberately tight: the payload is
// one small JSON document and the receiver replies before doing any work.
const exportTimeout = time.Second

// Export posts the event to the usage-stats receiver as a flat JSON body.
// The Event json tags are the wire contract (pinned by TestEventFieldInventory
// and by the receiver's own tests); the receiver stamps receipt time itself,
// so the payload carries no timestamp. Export never reports failure:
// telemetry is fire-and-forget and must not affect the command's outcome. A
// lost event is fine: the export is a single attempt with no retries, so an
// unreachable endpoint costs one fast failure rather than the full
// exportTimeout, which caps the whole exchange.
func Export(event Event) {
	endpoint := defaultEndpoint
	if override := os.Getenv(envEndpoint); override != "" {
		endpoint = override
	}
	export(event, endpoint)
}

func export(event Event, endpoint string) {
	body, err := json.Marshal(event)
	if err != nil {
		return
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", version.UserAgent())
	// Deliberately not the shared httputils client: its retry transport would
	// burn the whole exportTimeout budget on an unreachable endpoint, and this
	// runs synchronously before exit on every telemetry-enabled invocation.
	client := &http.Client{Timeout: exportTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	_ = resp.Body.Close()
}
