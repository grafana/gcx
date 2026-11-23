package scim

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/httputils"
	"github.com/grafana/gcx/internal/providers/native"
	"github.com/grafana/gcx/internal/retry"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/rest"
)

const (
	userSchema           = "urn:ietf:params:scim:schemas:core:2.0:User"
	patchSchema          = "urn:ietf:params:scim:api:messages:2.0:PatchOp"
	maxTokenBytes        = 1024 * 1024
	maxUserResponseBytes = 16 * 1024 * 1024
)

// userResource retains all server fields, including SCIM extensions.
type userResource map[string]any

type usersClient struct {
	base, token string
	http        *http.Client
}

// readUserToken follows the probe token reader contract. Only line endings are removed.
func readUserToken(r io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxTokenBytes+1))
	if err != nil {
		return "", fmt.Errorf("read SCIM token: %w", err)
	}
	if len(data) > maxTokenBytes {
		return "", errors.New("SCIM token exceeds 1 MiB")
	}
	token := strings.TrimRight(string(data), "\r\n")
	if token == "" {
		return "", errors.New("SCIM token is empty")
	}
	return token, nil
}

func loadUsersClient(ctx context.Context, binding native.Binding, token string, requireUserSync bool) (*usersClient, error) {
	access, err := binding.Load(ctx, native.LoadOptions{})
	if err != nil {
		return nil, err
	}
	item, err := access.Client.Get(ctx, access.Descriptor, "default", metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if requireUserSync {
		enabled, _, err := unstructured.NestedBool(item.Object, "spec", "enableUserSync")
		if err != nil {
			return nil, errors.New("invalid SCIM user sync setting: expected a boolean")
		}
		if !enabled {
			return nil, errors.New("SCIM user sync is disabled; run 'gcx scim settings update --user-sync=true'")
		}
	}
	base, err := tenantEndpoint(access.Config.GrafanaURL, item.GetNamespace(), access.Descriptor.GroupVersion.Version)
	if err != nil {
		return nil, err
	}
	// Use TLS settings only. Context authentication and OAuth proxy wrappers must not reach this client.
	tlsConfig, err := rest.TLSConfigFor(&rest.Config{TLSClientConfig: access.Config.TLSClientConfig})
	if err != nil {
		return nil, fmt.Errorf("load SCIM TLS settings: %w", err)
	}
	middlewares := []httputils.Middleware{httputils.LoggingMiddleware}
	if httputils.PayloadLogging(ctx) {
		middlewares = []httputils.Middleware{httputils.RequestResponseLoggingMiddleware, httputils.LoggingMiddleware}
	}
	client := httputils.NewClient(httputils.ClientOpts{TLSConfig: tlsConfig, Middlewares: middlewares, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }})
	return &usersClient{base: base, token: token, http: client}, nil
}

func (c *usersClient) request(ctx context.Context, method, suffix string, query url.Values, body any) (userResource, error) {
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encode SCIM request: %w", err)
		}
	}
	endpoint := c.base + "/Users" + suffix
	if len(query) != 0 {
		endpoint += "?" + query.Encode()
	}
	// A connection error can occur after the server applies a write. Do not replay it.
	if method == http.MethodPost || method == http.MethodPatch {
		ctx = retry.WithoutRetries(ctx)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create SCIM request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/scim+json")
	if body != nil {
		req.Header.Set("Content-Type", "application/scim+json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("SCIM %s request failed: %w", method, err)
	}
	defer resp.Body.Close()
	data, err = io.ReadAll(io.LimitReader(resp.Body, maxUserResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read SCIM response: %w", err)
	}
	if len(data) > maxUserResponseBytes {
		return nil, errors.New("SCIM response exceeds 16 MiB")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var detail struct {
			Detail   string `json:"detail"`
			SCIMType string `json:"scimType"`
		}
		_ = json.Unmarshal(data, &detail)
		message := fmt.Sprintf("SCIM %s failed (HTTP %d)", method, resp.StatusCode)
		if detail.SCIMType != "" {
			message += ": " + detail.SCIMType
		}
		if detail.Detail != "" {
			message += ": " + detail.Detail
		}
		return nil, errors.New(strings.ReplaceAll(message, c.token, "[redacted]"))
	}
	if resp.StatusCode == http.StatusNoContent {
		if method != http.MethodPatch {
			return nil, errors.New("SCIM read or create returned no resource")
		}
		return nil, nil //nolint:nilnil // SCIM PATCH can succeed with HTTP 204 and no resource.
	}
	var result userResource
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decode SCIM response: %w", err)
	}
	if result == nil {
		return nil, errors.New("SCIM response must be an object")
	}
	return result, nil
}

func (c *usersClient) get(ctx context.Context, id string) (userResource, error) {
	return c.request(ctx, http.MethodGet, "/"+url.PathEscape(id), nil, nil)
}

func (c *usersClient) list(ctx context.Context, filter string) ([]userResource, error) {
	users := make([]userResource, 0)
	seen := make(map[string]bool)
	start := 1
	total := -1
	for range 10000 {
		query := url.Values{"startIndex": {strconv.Itoa(start)}, "count": {"100"}}
		if filter != "" {
			query.Set("filter", filter)
		}
		result, err := c.request(ctx, http.MethodGet, "", query, nil)
		if err != nil {
			return nil, err
		}
		data, err := json.Marshal(result)
		if err != nil {
			return nil, err
		}
		var page struct {
			Resources    []userResource `json:"Resources"`
			TotalResults *int           `json:"totalResults"`
			StartIndex   *int           `json:"startIndex"`
			ItemsPerPage *int           `json:"itemsPerPage"`
		}
		if err := json.Unmarshal(data, &page); err != nil {
			return nil, fmt.Errorf("decode SCIM user page: %w", err)
		}
		if page.TotalResults == nil || *page.TotalResults < 0 {
			return nil, errors.New("SCIM user page has no valid totalResults; completeness cannot be verified")
		}
		if total != -1 && total != *page.TotalResults {
			return nil, errors.New("SCIM result count changed during pagination; run the list again")
		}
		total = *page.TotalResults
		if page.StartIndex != nil && *page.StartIndex != start {
			return nil, errors.New("SCIM pagination did not advance to the requested index")
		}
		if page.ItemsPerPage != nil && *page.ItemsPerPage != len(page.Resources) {
			return nil, errors.New("SCIM pagination itemsPerPage does not match Resources")
		}
		for _, user := range page.Resources {
			id, ok := user["id"].(string)
			if !ok || id == "" || seen[id] {
				return nil, errors.New("SCIM pagination returned a missing or repeated user ID")
			}
			seen[id] = true
			users = append(users, user)
		}
		if len(users) > *page.TotalResults {
			return nil, errors.New("SCIM pagination returned more users than totalResults")
		}
		if len(users) == *page.TotalResults {
			return users, nil
		}
		if len(page.Resources) == 0 {
			return nil, errors.New("SCIM pagination stopped before all users were returned")
		}
		start += len(page.Resources)
	}
	return nil, errors.New("SCIM pagination exceeds 10000 pages; use --filter to select fewer users")
}
