package secrets_test

import (
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/grafana/gcx/internal/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestRedactsQueryWithoutChangingOriginal(t *testing.T) {
	const rawURL = "https://artifacts.example.test/a.png?X-Amz-Credential=secret&X-Amz-Signature=signature"
	req, err := http.NewRequestWithContext(secrets.WithRedactedURLQuery(t.Context()), http.MethodGet, rawURL, nil)
	require.NoError(t, err)

	logRequest := secrets.Request(req)
	assert.Equal(t, "https://artifacts.example.test/a.png?REDACTED", logRequest.URL.String())
	assert.Equal(t, rawURL, req.URL.String())
	assert.Equal(t, logRequest.URL.String(), secrets.URLString(req.Context(), req.URL))
	assert.Equal(t,
		`Get "https://artifacts.example.test/a.png?REDACTED": request failed for REDACTED`,
		secrets.ErrorString(req.Context(), req.URL, errors.New(`Get "`+rawURL+`": request failed for `+req.URL.RawQuery)),
	)
	redirectErr := &url.Error{
		Op:  "Get",
		URL: "https://redirect.example.test/a.png?token=redirect-secret",
		Err: errors.New("connection refused"),
	}
	assert.Equal(t,
		`Get "https://redirect.example.test/a.png?REDACTED": connection refused`,
		secrets.ErrorString(req.Context(), req.URL, redirectErr),
	)
}
