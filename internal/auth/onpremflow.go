// onpremflow implements the browser-based authentication flow for gcx with
// On-Prem instances of Grafana.
// The companion plugin joshuagrisham-gcxonpremoauth-app is required.
package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/user"
	"strings"
	"sync"
	"time"
)

// OnPremFlowOptions configures the on-prem browser login.
type OnPremFlowOptions struct {
	// Port specifies a fixed port for the callback server.
	// If 0, an available port will be found automatically.
	Port int

	// BindAddress specifies the address to bind the callback server to.
	// Defaults to "127.0.0.1".
	BindAddress string

	// OrgID is the Organization ID where the user's token should be created.
	OrgID int64

	// Writer is the output writer for user-facing messages.
	// Defaults to os.Stderr.
	Writer io.Writer
}

// onPremPluginID is the fixed Grafana plugin ID. Not configurable;
// the plugin is always installed under this ID.
const onPremPluginID = "joshuagrisham-gcxonpremoauth-app"

// OnPremFlow manages the browser handshake with joshuagrisham-gcxonpremoauth-app.
type OnPremFlow struct {
	endpoint string
	opts     OnPremFlowOptions
	writer   io.Writer
}

// NewOnPremFlow constructs a new flow for the given Grafana server URL.
func NewOnPremFlow(endpoint string, opts OnPremFlowOptions) *OnPremFlow {
	if opts.BindAddress == "" {
		opts.BindAddress = "127.0.0.1"
	}
	w := opts.Writer
	if w == nil {
		w = os.Stderr
	}
	return &OnPremFlow{endpoint: endpoint, opts: opts, writer: w}
}

// Run executes the flow and returns the captured token.
func (f *OnPremFlow) Run(ctx context.Context) (*Result, error) {
	if f.endpoint == "" {
		return nil, errors.New("server URL is required")
	}

	listener, port, err := listenOnCallbackPort(ctx, f.opts.BindAddress, f.opts.Port)
	if err != nil {
		if f.opts.Port == 0 {
			return nil, fmt.Errorf("no available port: %w", err)
		}
		return nil, err
	}

	nonce, err := generateState()
	if err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	resultCh := make(chan *Result, 1)
	errCh := make(chan error, 1)
	server := f.startCallbackServer(listener, nonce, resultCh, errCh)

	defer func() { //nolint:contextcheck // intentionally use Background for graceful shutdown after ctx cancellation
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	tokenName := "gcx"
	if currentUser, err := user.Current(); err == nil && currentUser.Username != "" {
		tokenName += "-" + currentUser.Username
	}
	if hostname, err := os.Hostname(); err == nil && hostname != "" {
		tokenName += "-" + hostname
	}
	tokenName += "-" + time.Now().UTC().Format("20060102150405")

	authEndpoint := strings.TrimSuffix(f.endpoint, "/")

	authURL := fmt.Sprintf("%s/a/%s/authorize?orgId=%d&callback_port=%d&nonce=%s&name=%s",
		authEndpoint, onPremPluginID, f.opts.OrgID, port, url.QueryEscape(nonce), url.QueryEscape(tokenName))

	// TODO: Do we also want users to be able to set secondsToLive ?

	fmt.Fprintln(f.writer, "Opening browser to authenticate...")
	fmt.Fprintf(f.writer, "If browser doesn't open, visit:\n  %s\n\n", authURL)

	if err := openBrowser(ctx, authURL); err != nil {
		fmt.Fprintln(f.writer, "(Could not open browser automatically)")
	}

	fmt.Fprintln(f.writer, "Waiting for authentication...")

	select {
	case result := <-resultCh:
		return result, nil
	case err := <-errCh:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (f *OnPremFlow) startCallbackServer(listener net.Listener, nonce string, resultCh chan<- *Result, errCh chan<- error) *http.Server {
	var once sync.Once

	mux := http.NewServeMux()

	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		handled := false
		once.Do(func() {
			handled = true
			if nonce != r.FormValue("nonce") {
				errCh <- errors.New("nonce mismatch - possible CSRF attack")
				renderErrorPage(w, "Invalid nonce")
				return
			}
			if r.FormValue("token") == "" {
				errCh <- errors.New("missing token")
				renderErrorPage(w, "Missing token")
				return
			}

			result := &Result{
				Token: r.FormValue("token"),
			}

			resultCh <- result
			renderSuccessPage(w)
		})
		if !handled {
			http.Error(w, "Authentication already processed", http.StatusGone)
		}
	})

	server := &http.Server{
		Addr:              listener.Addr().String(),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("callback server error: %w", err)
		}
	}()

	return server
}
