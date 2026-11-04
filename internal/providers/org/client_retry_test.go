package org //nolint:testpackage // Tests cover the shared private request helper.

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/grafana/gcx/internal/httputils"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOrgRequestsAfterConnectionReset(t *testing.T) {
	tests := []struct {
		method       string
		wantAttempts int
		wantError    bool
	}{
		{http.MethodPost, 1, true},
		{http.MethodPatch, 1, true},
		{http.MethodDelete, 1, true},
		{http.MethodGet, 2, false},
	}
	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			var attempts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				assert.NoError(t, err)
				if tt.method == http.MethodPost || tt.method == http.MethodPatch {
					assert.JSONEq(t, `{"name":"robot"}`, string(body))
				}
				if attempts.Add(1) > 1 {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				// Read the full body before resetting the connection. The write may be complete.
				connection, _, err := http.NewResponseController(w).Hijack()
				if !assert.NoError(t, err) {
					return
				}
				tcp, ok := connection.(*net.TCPConn)
				if assert.True(t, ok) {
					assert.NoError(t, tcp.SetLinger(0))
				}
				assert.NoError(t, connection.Close())
			}))
			defer server.Close()
			c := &client{deps: adapter.ClientDeps{BaseURL: server.URL, HTTP: httputils.NewDefaultClient(t.Context())}}
			var input any
			if tt.method == http.MethodPost || tt.method == http.MethodPatch {
				input = map[string]string{"name": "robot"}
			}
			err := c.send(t.Context(), tt.method, "/api/example", input, nil)
			if tt.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.EqualValues(t, tt.wantAttempts, attempts.Load())
		})
	}
}
