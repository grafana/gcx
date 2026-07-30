package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/grafana/gcx/internal/providers"
	"sigs.k8s.io/yaml"
)

// bodyCodec bundles the wire encoding used by an API family.
type bodyCodec struct {
	contentType string
	marshal     func(any) ([]byte, error)
	unmarshal   func([]byte, any) error
}

func jsonBodyCodec() bodyCodec {
	return bodyCodec{"application/json", json.Marshal, json.Unmarshal}
}

func yamlBodyCodec() bodyCodec {
	return bodyCodec{
		contentType: "application/yaml",
		marshal:     yaml.Marshal,
		unmarshal:   func(data []byte, out any) error { return yaml.Unmarshal(data, out) },
	}
}

// doBody performs an HTTP request with an optional encoded body and
// optionally decodes the response. A 2xx with an empty body is success.
// 404 responses are wrapped around notFound; other >= 400 responses go
// through providers.HandleErrorResponse. path is the logical API path used
// in error messages (the full URL may include proxy prefixes).
func doBody(ctx context.Context, httpClient *http.Client, method, url, path string, codec bodyCodec, notFound error, in, out any) error {
	var body io.Reader
	if in != nil {
		data, err := codec.marshal(in)
		if err != nil {
			return fmt.Errorf("failed to marshal request body: %w", err)
		}
		body = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	if in != nil {
		req.Header.Set("Content-Type", codec.contentType)
	}
	req.Header.Set("Accept", codec.contentType)

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return fmt.Errorf("%s %s: %w", method, path, notFound)
	case resp.StatusCode >= 400:
		return providers.HandleErrorResponse(resp)
	}

	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}
	if len(data) == 0 {
		return nil
	}
	if err := codec.unmarshal(data, out); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}
	return nil
}
