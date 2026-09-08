package k6

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/httputils"
)

const (
	pluginProxyLogsBasePath     = "/api/plugins/k6-app/resources/logs"
	pluginProxyInsightsBasePath = "/api/plugins/k6-app/resources/insights"
	defaultLogsDomain           = "https://cloudlogs.k6.io"
)

type cloudTarget uint8

const (
	cloudTargetCloud cloudTarget = iota
	cloudTargetLogs
	cloudTargetInsights
)

type cloudAuthRoute uint8

const (
	cloudAuthConfigured cloudAuthRoute = iota
	cloudAuthDirectStackID
	cloudAuthDirectStackURL
)

type cloudRequest struct {
	Target      cloudTarget
	Auth        cloudAuthRoute
	Method      string
	Path        string
	Body        []byte
	ContentType string
	Accept      string
	Headers     http.Header
}

type cloudResponse struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

type cloudExecutor interface {
	doCloud(ctx context.Context, request cloudRequest) (cloudResponse, error)
}

type cloudOperations struct {
	executor cloudExecutor
}

func (c *ProxyClient) doCloud(ctx context.Context, request cloudRequest) (cloudResponse, error) {
	if err := validateCloudRequest(request); err != nil {
		return cloudResponse{}, err
	}
	if request.Auth == cloudAuthConfigured {
		return c.doProxyCloud(ctx, request)
	}
	return c.doDirectCloud(ctx, request)
}

func (c *ProxyClient) doProxyCloud(ctx context.Context, request cloudRequest) (cloudResponse, error) {
	base, err := c.proxyTargetBase(request.Target)
	if err != nil {
		return cloudResponse{}, err
	}
	req, err := buildCloudRequest(ctx, base, request)
	if err != nil {
		return cloudResponse{}, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return cloudResponse{}, err
	}
	return readCloudResponse(resp)
}

func (c *ProxyClient) doDirectCloud(ctx context.Context, request cloudRequest) (cloudResponse, error) {
	token, err := c.Token(ctx)
	if err != nil {
		return cloudResponse{}, err
	}
	resp, err := c.sendDirectCloud(ctx, request, token)
	if err != nil {
		return cloudResponse{}, err
	}
	if resp == nil {
		return readCloudResponse(nil)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		return readCloudResponse(resp)
	}
	_, _ = io.CopyN(io.Discard, resp.Body, 1<<20)
	_ = resp.Body.Close()

	c.invalidateToken(token)
	refreshedToken, err := c.Token(ctx)
	if err != nil {
		return cloudResponse{}, fmt.Errorf("k6: refresh direct API token: %w", err)
	}
	resp, err = c.sendDirectCloud(ctx, request, refreshedToken)
	if err != nil {
		return cloudResponse{}, err
	}
	return readCloudResponse(resp)
}

func (c *ProxyClient) sendDirectCloud(ctx context.Context, request cloudRequest, token string) (*http.Response, error) {
	base, err := c.directTargetBase(request.Target)
	if err != nil {
		return nil, err
	}
	req, err := buildCloudRequest(ctx, base, request)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	switch request.Auth {
	case cloudAuthDirectStackID:
		if c.stackID <= 0 {
			return nil, errors.New("k6: stack ID is required for a direct API request")
		}
		req.Header.Set("X-Stack-Id", strconv.Itoa(c.stackID))
	case cloudAuthDirectStackURL:
		if c.stackURL == "" {
			return nil, errors.New("k6: stack URL is required for authentication validation")
		}
		req.Header.Set("X-Stack-Url", c.stackURL)
	default:
		return nil, fmt.Errorf("k6: unsupported direct auth route %d", request.Auth)
	}
	return c.directHTTP.Do(req)
}

func (c *ProxyClient) proxyTargetBase(target cloudTarget) (string, error) {
	switch target {
	case cloudTargetCloud:
		return c.host + pluginProxyBasePath, nil
	case cloudTargetLogs:
		return c.host + pluginProxyLogsBasePath, nil
	case cloudTargetInsights:
		return c.host + pluginProxyInsightsBasePath, nil
	default:
		return "", fmt.Errorf("k6: unsupported API target %d", target)
	}
}

func (c *ProxyClient) directTargetBase(target cloudTarget) (string, error) {
	switch target {
	case cloudTargetCloud, cloudTargetInsights:
		return c.apiDomain, nil
	case cloudTargetLogs:
		return c.logsDomain, nil
	default:
		return "", fmt.Errorf("k6: unsupported API target %d", target)
	}
}

func (c *DirectClient) doCloud(ctx context.Context, request cloudRequest) (cloudResponse, error) {
	if err := validateCloudRequest(request); err != nil {
		return cloudResponse{}, err
	}
	base, err := c.directTargetBase(request.Target)
	if err != nil {
		return cloudResponse{}, err
	}
	build := func() (*http.Request, error) {
		c.mu.Lock()
		token := c.token
		stackID := c.stackID
		stackURL := c.stackURL
		c.mu.Unlock()

		req, err := buildCloudRequest(ctx, base, request)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		if request.Auth == cloudAuthDirectStackURL {
			if stackURL == "" {
				return nil, errors.New("k6: stack URL is required for authentication validation")
			}
			req.Header.Set("X-Stack-Url", stackURL)
		} else {
			req.Header.Set("X-Stack-Id", strconv.Itoa(stackID))
		}
		return req, nil
	}
	resp, err := c.doWithReauth(ctx, build)
	if err != nil {
		return cloudResponse{}, err
	}
	return readCloudResponse(resp)
}

func validateCloudRequest(request cloudRequest) error {
	if request.Method == "" {
		return errors.New("k6: HTTP method is required")
	}
	if !strings.HasPrefix(request.Path, "/") {
		return fmt.Errorf("k6: API path %q must start with '/'", request.Path)
	}
	switch request.Auth {
	case cloudAuthConfigured, cloudAuthDirectStackID:
		return nil
	case cloudAuthDirectStackURL:
		if request.Target != cloudTargetCloud {
			return errors.New("k6: stack URL authentication is supported only for the Cloud API")
		}
		return nil
	default:
		return fmt.Errorf("k6: unsupported auth route %d", request.Auth)
	}
}

func (c *DirectClient) directTargetBase(target cloudTarget) (string, error) {
	switch target {
	case cloudTargetCloud, cloudTargetInsights:
		return c.apiDomain, nil
	case cloudTargetLogs:
		return c.logsDomain, nil
	default:
		return "", fmt.Errorf("k6: unsupported API target %d", target)
	}
}

func buildCloudRequest(ctx context.Context, base string, request cloudRequest) (*http.Request, error) {
	var body io.Reader
	if request.Body != nil {
		body = bytes.NewReader(request.Body)
	}
	req, err := http.NewRequestWithContext(ctx, request.Method, strings.TrimRight(base, "/")+request.Path, body)
	if err != nil {
		return nil, fmt.Errorf("k6: create request: %w", err)
	}
	for key, values := range request.Headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	if request.ContentType != "" {
		req.Header.Set("Content-Type", request.ContentType)
	}
	if request.Accept != "" {
		req.Header.Set("Accept", request.Accept)
	}
	return req, nil
}

func readCloudResponse(resp *http.Response) (cloudResponse, error) {
	if resp == nil {
		return cloudResponse{}, errors.New("k6: API returned no HTTP response")
	}
	defer resp.Body.Close()
	body, err := httputils.ReadResponseBody(resp.Body, httputils.DefaultResponseLimit)
	if err != nil {
		return cloudResponse{}, fmt.Errorf("k6: read response: %w", err)
	}
	return cloudResponse{
		StatusCode: resp.StatusCode,
		Header:     resp.Header.Clone(),
		Body:       body,
	}, nil
}

func asHTTPResponse(response cloudResponse) *http.Response {
	return &http.Response{
		StatusCode: response.StatusCode,
		Header:     response.Header,
		Body:       io.NopCloser(bytes.NewReader(response.Body)),
	}
}
