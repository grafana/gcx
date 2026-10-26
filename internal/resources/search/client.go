package search

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/gcxerrors"
	"k8s.io/client-go/rest"
)

const apiVersion = "search.grafana.app/v0alpha1"

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
	const route = "/apis/search.grafana.app/v0alpha1/namespaces/"
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
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		message := strings.TrimSpace(string(data))
		var status struct {
			Message string `json:"message"`
			TraceID string `json:"traceID"`
		}
		if json.Unmarshal(data, &status) == nil && status.Message != "" {
			message = status.Message
		}
		if message == "" {
			message = resp.Status
		}
		return nil, &gcxerrors.HTTPStatusError{Status: resp.StatusCode, Message: fmt.Sprintf("hybrid search failed: HTTP %d: %s", resp.StatusCode, message), ServerMessage: message, TraceID: status.TraceID, ContentType: resp.Header.Get("Content-Type")}
	}
	var result Results
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&result); err != nil {
		return nil, fmt.Errorf("decode hybrid search response: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, errors.New("invalid hybrid search response: trailing data")
	}
	if result.APIVersion != apiVersion || result.Kind != "HybridSearchResults" || result.Items == nil {
		return nil, errors.New("invalid hybrid search response: expected HybridSearchResults envelope with items")
	}
	return &result, nil
}
