// Package cookierefresh implements browser-based cookie refresh for edge-proxy
// authentication (e.g. AWS ALB OIDC) using Chrome DevTools Protocol.
package cookierefresh

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"

	"github.com/grafana/gcx/internal/config"
)

const defaultTimeout = 5 * time.Minute

// Refresh opens a visible Chrome window to cfg.TriggerURL, waits for the user
// to complete the authentication flow, captures the cookie named cfg.CookieName,
// and returns its value. The caller is responsible for writing it back to config.
func Refresh(ctx context.Context, cfg *config.CookieRefreshConfig) (string, error) {
	if err := cfg.Validate(); err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	// Run non-headless so the user can interact with the auth flow.
	allocOptions := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", false),
		chromedp.Flag("disable-gpu", false),
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(ctx, allocOptions...)
	defer allocCancel()

	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	defer browserCancel()

	if err := chromedp.Run(browserCtx, chromedp.Navigate(cfg.TriggerURL)); err != nil {
		if strings.Contains(err.Error(), "exec") || strings.Contains(err.Error(), "executable") {
			return "", errors.New("Chrome not found: install Google Chrome or Chromium and retry")
		}
		return "", fmt.Errorf("opening browser: %w", err)
	}

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("timed out after %s waiting for %q cookie — complete the login in the browser window", defaultTimeout, cfg.CookieName)
		case <-ticker.C:
			value, found, err := findCookie(browserCtx, cfg)
			if err != nil {
				continue // browser may be mid-navigation; retry
			}
			if found {
				return value, nil
			}
		}
	}
}

// findCookie fetches all browser cookies and returns the value of the first
// cookie matching cfg.CookieName. When cfg.CallbackPath is set, the cookie is
// only considered once the current URL contains that path.
func findCookie(ctx context.Context, cfg *config.CookieRefreshConfig) (value string, found bool, err error) {
	var cookies []*network.Cookie
	var currentURL string

	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var innerError error
		cookies, innerError = network.GetCookies().Do(ctx)
		return innerError
	})); err != nil {
		return "", false, err
	}

	if cfg.CallbackPath != "" {
		if err := chromedp.Run(ctx, chromedp.Location(&currentURL)); err != nil {
			return "", false, err
		}
		if !strings.Contains(currentURL, cfg.CallbackPath) {
			return "", false, nil
		}
	}

	for _, c := range cookies {
		if c.Name == cfg.CookieName {
			return c.Value, true, nil
		}
	}
	return "", false, nil
}
