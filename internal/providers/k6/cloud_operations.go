package k6

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"slices"
	"strings"
)

func checkCloudStatus(response cloudResponse, operation string, allowed ...int) error {
	if slices.Contains(allowed, response.StatusCode) {
		return nil
	}
	message := strings.TrimSpace(string(response.Body))
	if message == "" {
		message = http.StatusText(response.StatusCode)
	}
	return fmt.Errorf("k6: %s: status %d: %s", operation, response.StatusCode, message)
}

func decodeCloudJSON[T any](response cloudResponse) (T, error) {
	var result T
	if err := json.Unmarshal(response.Body, &result); err != nil {
		return result, fmt.Errorf("k6: decode response: %w", err)
	}
	return result, nil
}

// ListLoadTests retrieves all load tests across all projects.
func (o *cloudOperations) ListLoadTests(ctx context.Context) ([]LoadTest, error) {
	return o.listLoadTests(ctx, loadTestsPath, 0)
}

// ListLoadTestsWithLimit retrieves at most limit load tests.
func (o *cloudOperations) ListLoadTestsWithLimit(ctx context.Context, limit int) ([]LoadTest, error) {
	return o.listLoadTests(ctx, loadTestsPath, limit)
}

// ListLoadTestsByProject retrieves load tests through the project-scoped v6 route.
func (o *cloudOperations) ListLoadTestsByProject(ctx context.Context, projectID int) ([]LoadTest, error) {
	path := fmt.Sprintf(projectsPath+"/%d/load_tests", projectID)
	return o.listLoadTests(ctx, path, 0)
}

func (o *cloudOperations) listLoadTests(ctx context.Context, path string, limit int) ([]LoadTest, error) {
	const defaultPageSize = 100
	all := make([]LoadTest, 0)

	for {
		pageSize := defaultPageSize
		if limit > 0 {
			if remaining := limit - len(all); remaining < pageSize {
				pageSize = remaining
			}
		}
		separator := "?"
		for _, char := range path {
			if char == '?' {
				separator = "&"
				break
			}
		}
		pagePath := fmt.Sprintf("%s%s$skip=%d&$top=%d", path, separator, len(all), pageSize)
		response, err := o.executor.doCloud(ctx, cloudRequest{
			Target: cloudTargetCloud,
			Auth:   cloudAuthConfigured,
			Method: http.MethodGet,
			Path:   pagePath,
			Accept: "application/json",
		})
		if err != nil {
			return nil, fmt.Errorf("k6: list load tests: %w", err)
		}
		if err := checkCloudStatus(response, "list load tests", http.StatusOK); err != nil {
			return nil, err
		}
		page, err := decodeCloudJSON[loadTestsResponse](response)
		if err != nil {
			return nil, err
		}
		all = append(all, page.Value...)
		if limit > 0 && len(all) >= limit {
			return all[:limit], nil
		}
		if len(page.Value) < pageSize || (page.Count > 0 && len(all) >= page.Count) {
			return all, nil
		}
	}
}

// CreateLoadTest creates a new load test through the direct k6 endpoint.
// The Grafana plugin proxy does not preserve multipart request content types.
func (o *cloudOperations) CreateLoadTest(ctx context.Context, name string, projectID int, script string) (*LoadTest, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("name", name); err != nil {
		return nil, fmt.Errorf("k6: write name field: %w", err)
	}
	part, err := writer.CreateFormFile("script", "script.js")
	if err != nil {
		return nil, fmt.Errorf("k6: create script form file: %w", err)
	}
	if _, err := io.WriteString(part, script); err != nil {
		return nil, fmt.Errorf("k6: write script content: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("k6: close multipart writer: %w", err)
	}

	authRoute := cloudAuthDirectStackID
	if proxy, ok := o.executor.(*ProxyClient); ok && proxy.selectedStackID() <= 0 {
		authRoute = cloudAuthConfigured
	}
	response, err := o.executor.doCloud(ctx, cloudRequest{
		Target:      cloudTargetCloud,
		Auth:        authRoute,
		Method:      http.MethodPost,
		Path:        fmt.Sprintf(projectsPath+"/%d/load_tests", projectID),
		Body:        body.Bytes(),
		ContentType: writer.FormDataContentType(),
		Accept:      "application/json",
	})
	if err != nil {
		return nil, fmt.Errorf("k6: create load test: %w", err)
	}
	if err := checkCloudStatus(response, "create load test", http.StatusCreated, http.StatusOK); err != nil {
		return nil, err
	}
	loadTest, err := decodeCloudJSON[LoadTest](response)
	if err != nil {
		return nil, err
	}
	return &loadTest, nil
}

// ListAllowedProjects lists projects that can use a private load zone.
func (o *cloudOperations) ListAllowedProjects(ctx context.Context, loadZoneID int) ([]AllowedProject, error) {
	response, err := o.executor.doCloud(ctx, cloudRequest{
		Target: cloudTargetCloud,
		Auth:   cloudAuthConfigured,
		Method: http.MethodGet,
		Path:   fmt.Sprintf(loadZonesPath+"/%d/allowed_projects", loadZoneID),
		Accept: "application/json",
	})
	if err != nil {
		return nil, fmt.Errorf("k6: list allowed projects: %w", err)
	}
	if err := checkCloudStatus(response, "list allowed projects", http.StatusOK); err != nil {
		return nil, err
	}
	result, err := decodeCloudJSON[allowedProjectsResponse](response)
	if err != nil {
		return nil, err
	}
	return result.Value, nil
}

// UpdateAllowedProjects replaces the projects that can use a private load zone.
func (o *cloudOperations) UpdateAllowedProjects(ctx context.Context, loadZoneID int, projectIDs []int) error {
	if err := validateAllowedIDs("project", projectIDs); err != nil {
		return err
	}
	body, err := json.Marshal(newAllowedIDsRequest(projectIDs))
	if err != nil {
		return fmt.Errorf("k6: marshal allowed projects: %w", err)
	}
	response, err := o.executor.doCloud(ctx, cloudRequest{
		Target:      cloudTargetCloud,
		Auth:        cloudAuthConfigured,
		Method:      http.MethodPut,
		Path:        fmt.Sprintf(loadZonesPath+"/%d/allowed_projects", loadZoneID),
		Body:        body,
		ContentType: "application/json",
		Accept:      "application/json",
	})
	if err != nil {
		return fmt.Errorf("k6: update allowed projects: %w", err)
	}
	return checkCloudStatus(response, "update allowed projects", http.StatusOK, http.StatusNoContent)
}

// ListAllowedLoadZones lists private load zones that a project can use.
func (o *cloudOperations) ListAllowedLoadZones(ctx context.Context, projectID int) ([]AllowedLoadZone, error) {
	response, err := o.executor.doCloud(ctx, cloudRequest{
		Target: cloudTargetCloud,
		Auth:   cloudAuthConfigured,
		Method: http.MethodGet,
		Path:   fmt.Sprintf(projectsPath+"/%d/allowed_load_zones", projectID),
		Accept: "application/json",
	})
	if err != nil {
		return nil, fmt.Errorf("k6: list allowed load zones: %w", err)
	}
	if err := checkCloudStatus(response, "list allowed load zones", http.StatusOK); err != nil {
		return nil, err
	}
	result, err := decodeCloudJSON[allowedLoadZonesResponse](response)
	if err != nil {
		return nil, err
	}
	return result.Value, nil
}

// UpdateAllowedLoadZones replaces the private load zones that a project can use.
func (o *cloudOperations) UpdateAllowedLoadZones(ctx context.Context, projectID int, loadZoneIDs []int) error {
	if err := validateAllowedIDs("load zone", loadZoneIDs); err != nil {
		return err
	}
	body, err := json.Marshal(newAllowedIDsRequest(loadZoneIDs))
	if err != nil {
		return fmt.Errorf("k6: marshal allowed load zones: %w", err)
	}
	response, err := o.executor.doCloud(ctx, cloudRequest{
		Target:      cloudTargetCloud,
		Auth:        cloudAuthConfigured,
		Method:      http.MethodPut,
		Path:        fmt.Sprintf(projectsPath+"/%d/allowed_load_zones", projectID),
		Body:        body,
		ContentType: "application/json",
		Accept:      "application/json",
	})
	if err != nil {
		return fmt.Errorf("k6: update allowed load zones: %w", err)
	}
	return checkCloudStatus(response, "update allowed load zones", http.StatusOK, http.StatusNoContent)
}

type allowedID struct {
	ID int `json:"id"`
}

type allowedIDsRequest struct {
	Value []allowedID `json:"value"`
}

func newAllowedIDsRequest(ids []int) allowedIDsRequest {
	values := make([]allowedID, len(ids))
	for i, id := range ids {
		values[i] = allowedID{ID: id}
	}
	return allowedIDsRequest{Value: values}
}

func validateAllowedIDs(kind string, ids []int) error {
	if len(ids) > 500 {
		return fmt.Errorf("k6: too many allowed %s IDs: got %d, maximum is 500", kind, len(ids))
	}
	for _, id := range ids {
		if id <= 0 {
			return fmt.Errorf("k6: invalid allowed %s ID %d: must be greater than 0", kind, id)
		}
	}
	return nil
}
