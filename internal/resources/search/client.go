package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/httputils"
	"k8s.io/client-go/rest"
)

const apiVersion = "search.grafana.app/v0alpha1"

const searchErrorBodyLimit = 64 << 10

const truncatedMarker = " ... [truncated] ... "

func shortenMessage(value string, limit int) string {
	if len(value) <= limit && utf8.ValidString(value) {
		return value
	}
	value = strings.ToValidUTF8(value, "\uFFFD")
	if len(value) <= limit {
		return value
	}
	available := limit - len(truncatedMarker)
	front := available / 2
	back := available - front
	for front > 0 && !utf8.RuneStart(value[front]) {
		front--
	}
	start := len(value) - back
	for start < len(value) && !utf8.RuneStart(value[start]) {
		start++
	}
	return value[:front] + truncatedMarker + value[start:]
}

type Client struct {
	http      *http.Client
	host      string
	namespace string
}

func NewClient(cfg config.NamespacedRESTConfig) (*Client, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("create hybrid search HTTP client: %w", err)
	}
	return &Client{http: httpClient, host: strings.TrimSuffix(cfg.Host, "/"), namespace: cfg.Namespace}, nil
}

func (c *Client) Search(ctx context.Context, query Query) (*Results, error) {
	query.APIVersion = apiVersion
	query.Kind = "HybridSearchQuery"
	body, err := json.Marshal(query)
	if err != nil {
		return nil, fmt.Errorf("encode hybrid search query: %w", err)
	}
	u, err := url.Parse(c.host)
	if err != nil {
		return nil, fmt.Errorf("parse hybrid search host: %w", err)
	}
	const route = "/apis/" + apiVersion + "/namespaces/"
	const tail = "/global/search/hybrid"
	rawPrefix := strings.TrimSuffix(u.EscapedPath(), "/")
	u.Path = strings.TrimSuffix(u.Path, "/") + route + c.namespace + tail
	u.RawPath = rawPrefix + route + url.PathEscape(c.namespace) + tail
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create hybrid search request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("perform hybrid search: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		data, readErr := httputils.ReadResponseBody(resp.Body, searchErrorBodyLimit)
		if readErr != nil {
			message := "could not read hybrid search error response: " + shortenMessage(readErr.Error(), 512)
			return nil, &gcxerrors.HTTPStatusError{Status: resp.StatusCode, Message: message, ServerMessage: message, ContentType: resp.Header.Get("Content-Type"), Cause: readErr}
		}
		message := strings.TrimSpace(string(data))
		var status struct {
			Message string `json:"message"`
			TraceID string `json:"traceID"`
		}
		if err := json.Unmarshal(data, &status); err != nil {
			status.TraceID = ""
		} else if status.Message != "" {
			message = status.Message
		}
		if message == "" {
			message = resp.Status
		}
		message = shortenMessage(message, 2048)
		status.TraceID = shortenMessage(status.TraceID, 256)
		return nil, &gcxerrors.HTTPStatusError{Status: resp.StatusCode, Message: fmt.Sprintf("hybrid search failed: HTTP %d: %s", resp.StatusCode, message), ServerMessage: message, TraceID: status.TraceID, ContentType: resp.Header.Get("Content-Type")}
	}
	var result Results
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&result); err != nil {
		return nil, fmt.Errorf("decode hybrid search response: %w", err)
	}
	if result.Kind != "HybridSearchResults" {
		return nil, fmt.Errorf("invalid hybrid search response: expected kind HybridSearchResults, got %q", result.Kind)
	}
	return &result, nil
}
