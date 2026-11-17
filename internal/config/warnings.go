package config

import (
	"context"
	"io"
	"sync"

	"github.com/grafana/gcx/internal/output"
	"k8s.io/client-go/rest"
)

// apiWarningHandler surfaces warnings returned by the Grafana API server (the Warning response
// header) on the command's diagnostic stream. Without it, client-go logs them through klog, which
// gcx only shows at -vvv, so warnings meant for the user, such as those from admission policies
// or deprecated APIs, were effectively hidden.
type apiWarningHandler struct {
	writer io.Writer

	mu   sync.Mutex
	seen map[string]struct{}
}

var _ rest.WarningHandlerWithContext = (*apiWarningHandler)(nil)

func newAPIWarningHandler(writer io.Writer) *apiWarningHandler {
	return &apiWarningHandler{writer: writer, seen: map[string]struct{}{}}
}

// HandleWarningHeaderWithContext emits each distinct warning once. Like client-go's own warning
// writer, it only handles code 299, the code Kubernetes uses for API warnings.
func (h *apiWarningHandler) HandleWarningHeaderWithContext(_ context.Context, code int, _ string, message string) {
	if code != 299 || message == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	// A batch operation can receive the same warning for many resources.
	if _, ok := h.seen[message]; ok {
		return
	}
	h.seen[message] = struct{}{}
	output.EmitWarn(h.writer, message)
}
