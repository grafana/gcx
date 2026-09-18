package k6

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	authV6Path          = "/cloud/v6/auth"
	labelKeysV6Path     = "/cloud/v6/labels"
	projectLimitsV6Path = "/cloud/v6/project-limits"
	testRunsV6Path      = "/cloud/v6/test_runs"
	validateOptionsPath = "/cloud/v6/validate_options"
)

func (o *cloudOperations) doV6JSON(ctx context.Context, method, path string, body, out any, accepted ...int) error {
	var raw []byte
	var err error
	if body != nil {
		raw, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("k6: encode request: %w", err)
		}
	}
	resp, err := o.executor.doCloud(ctx, cloudRequest{
		Target: cloudTargetCloud, Auth: cloudAuthConfigured, Method: method, Path: path,
		Body: raw, ContentType: "application/json", Accept: "application/json",
	})
	if err != nil {
		return err
	}
	if err := checkCloudStatus(resp, method+" "+path, accepted...); err != nil {
		return err
	}
	if out != nil && len(resp.Body) != 0 {
		if err := json.Unmarshal(resp.Body, out); err != nil {
			return fmt.Errorf("k6: decode %s %s response: %w", method, path, err)
		}
	}
	return nil
}

func (o *cloudOperations) ValidateCloudAuth(ctx context.Context) (*AuthValidation, error) {
	resp, err := o.executor.doCloud(ctx, cloudRequest{
		Target: cloudTargetCloud, Auth: cloudAuthDirectStackURL, Method: http.MethodGet, Path: authV6Path,
		Accept: "application/json",
	})
	if err != nil {
		return nil, err
	}
	if err := checkCloudStatus(resp, "validate Cloud authentication", http.StatusOK); err != nil {
		return nil, err
	}
	var result AuthValidation
	if err := json.Unmarshal(resp.Body, &result); err != nil {
		return nil, fmt.Errorf("k6: decode auth validation response: %w", err)
	}
	if source, ok := o.executor.(interface{ selectedStackID() int }); ok {
		expected := source.selectedStackID()
		if expected > 0 && result.StackID != expected {
			return nil, fmt.Errorf("k6: authentication context mismatch: selected stack %d, token stack %d", expected, result.StackID)
		}
	}
	return &result, nil
}

func (o *cloudOperations) ListLabelKeys(ctx context.Context) ([]LabelKey, error) {
	var response struct {
		Value []LabelKey `json:"value"`
	}
	if err := o.doV6JSON(ctx, http.MethodGet, labelKeysV6Path, nil, &response, http.StatusOK); err != nil {
		return nil, err
	}
	if response.Value == nil {
		response.Value = []LabelKey{}
	}
	return response.Value, nil
}

func (o *cloudOperations) CreateLabelKeys(ctx context.Context, req LabelKeyCreateRequest) ([]LabelKey, error) {
	var response struct {
		Value []LabelKey `json:"value"`
	}
	if err := o.doV6JSON(ctx, http.MethodPost, labelKeysV6Path, req, &response, http.StatusCreated); err != nil {
		return nil, err
	}
	if response.Value == nil {
		response.Value = []LabelKey{}
	}
	return response.Value, nil
}

func (o *cloudOperations) UpdateLabelKey(ctx context.Context, id int, req LabelKeyPatch) (*LabelKey, error) {
	var result LabelKey
	path := labelKeysV6Path + "/" + strconv.Itoa(id)
	if err := o.doV6JSON(ctx, http.MethodPatch, path, req, &result, http.StatusOK); err != nil {
		return nil, err
	}
	return &result, nil
}

func (o *cloudOperations) DeleteLabelKey(ctx context.Context, id int) error {
	return o.doV6JSON(ctx, http.MethodDelete, labelKeysV6Path+"/"+strconv.Itoa(id), nil, nil, http.StatusNoContent)
}

func (o *cloudOperations) MoveLoadTest(ctx context.Context, id, projectID int) error {
	return o.doV6JSON(ctx, http.MethodPut, fmt.Sprintf("%s/%d/move", loadTestsPath, id), map[string]int{"project_id": projectID}, nil, http.StatusNoContent)
}

func (o *cloudOperations) StartLoadTest(ctx context.Context, id int, idempotencyKey string) (*TestRun, error) {
	headers := http.Header{}
	if idempotencyKey != "" {
		headers.Set("K6-Idempotency-Key", idempotencyKey)
	}
	resp, err := o.executor.doCloud(ctx, cloudRequest{Target: cloudTargetCloud, Auth: cloudAuthConfigured, Method: http.MethodPost, Path: fmt.Sprintf("%s/%d/start", loadTestsPath, id), Accept: "application/json", Headers: headers})
	if err != nil {
		return nil, err
	}
	if err := checkCloudStatus(resp, "start load test", http.StatusOK); err != nil {
		return nil, err
	}
	var result TestRun
	if err := json.Unmarshal(resp.Body, &result); err != nil {
		return nil, fmt.Errorf("k6: decode start response: %w", err)
	}
	normalizeTestRun(&result)
	return &result, nil
}

func (o *cloudOperations) DownloadLoadTestScript(ctx context.Context, id int, accept string) (*ScriptDownload, error) {
	return o.downloadScript(ctx, fmt.Sprintf("%s/%d/script", loadTestsPath, id), accept)
}

func (o *cloudOperations) GetLoadTestSchedule(ctx context.Context, id int) (*CloudSchedule, error) {
	var result CloudSchedule
	path := fmt.Sprintf("%s/%d/schedule", loadTestsPath, id)
	if err := o.doV6JSON(ctx, http.MethodGet, path, nil, &result, http.StatusOK); err != nil {
		return nil, err
	}
	return &result, nil
}

func (o *cloudOperations) ValidateTestOptions(ctx context.Context, req ValidateOptionsRequest) (*ValidateOptionsResult, error) {
	var result ValidateOptionsResult
	if err := o.doV6JSON(ctx, http.MethodPost, validateOptionsPath, req, &result, http.StatusOK); err != nil {
		return nil, err
	}
	return &result, nil
}

func (o *cloudOperations) ListProjectLimits(ctx context.Context, projectIDs []int, top int) (*ProjectLimitsList, error) {
	query := url.Values{"$count": []string{"true"}}
	if top > 0 {
		query.Set("$top", strconv.Itoa(top))
	}
	if len(projectIDs) > 0 {
		parts := make([]string, len(projectIDs))
		for i, id := range projectIDs {
			parts[i] = strconv.Itoa(id)
		}
		query.Set("project_id_in", strings.Join(parts, ","))
	}
	var result ProjectLimitsList
	if err := o.doV6JSON(ctx, http.MethodGet, projectLimitsV6Path+"?"+query.Encode(), nil, &result, http.StatusOK); err != nil {
		return nil, err
	}
	if result.Value == nil {
		result.Value = []ProjectLimits{}
	}
	return &result, nil
}

func (o *cloudOperations) GetProjectLimits(ctx context.Context, id int) (*ProjectLimits, error) {
	var result ProjectLimits
	path := fmt.Sprintf("%s/%d/limits", projectsPath, id)
	if err := o.doV6JSON(ctx, http.MethodGet, path, nil, &result, http.StatusOK); err != nil {
		return nil, err
	}
	return &result, nil
}

func (o *cloudOperations) UpdateProjectLimits(ctx context.Context, id int, patch ProjectLimitsPatch) error {
	return o.doV6JSON(ctx, http.MethodPatch, fmt.Sprintf("%s/%d/limits", projectsPath, id), patch, nil, http.StatusNoContent)
}

func (o *cloudOperations) ListProjectLabels(ctx context.Context, id int) ([]ProjectLabel, error) {
	var response struct {
		Value []ProjectLabel `json:"value"`
	}
	if err := o.doV6JSON(ctx, http.MethodGet, fmt.Sprintf("%s/%d/labels", projectsPath, id), nil, &response, http.StatusOK); err != nil {
		return nil, err
	}
	if response.Value == nil {
		response.Value = []ProjectLabel{}
	}
	return response.Value, nil
}

func (o *cloudOperations) ReplaceProjectLabels(ctx context.Context, id int, req ProjectLabelPutRequest) ([]ProjectLabel, error) {
	var response struct {
		Value []ProjectLabel `json:"value"`
	}
	if err := o.doV6JSON(ctx, http.MethodPut, fmt.Sprintf("%s/%d/labels", projectsPath, id), req, &response, http.StatusOK); err != nil {
		return nil, err
	}
	if response.Value == nil {
		response.Value = []ProjectLabel{}
	}
	return response.Value, nil
}

func (o *cloudOperations) DeleteSchedule(ctx context.Context, id int) error {
	return o.doV6JSON(ctx, http.MethodDelete, fmt.Sprintf("%s/%d", schedulesPath, id), nil, nil, http.StatusNoContent)
}

func (o *cloudOperations) ActivateSchedule(ctx context.Context, id int) error {
	return o.doV6JSON(ctx, http.MethodPost, fmt.Sprintf("%s/%d/activate", schedulesPath, id), nil, nil, http.StatusNoContent)
}

func (o *cloudOperations) DeactivateSchedule(ctx context.Context, id int) error {
	return o.doV6JSON(ctx, http.MethodPost, fmt.Sprintf("%s/%d/deactivate", schedulesPath, id), nil, nil, http.StatusNoContent)
}

func (o *cloudOperations) ListAllTestRuns(ctx context.Context, params TestRunListParams) (*TestRunList, error) {
	const pageLimit = 100
	all := make([]TestRun, 0)
	skip := params.Skip
	total := 0
	for {
		pageSize := pageLimit
		if params.Top > 0 && params.Top-len(all) < pageSize {
			pageSize = params.Top - len(all)
		}
		if pageSize <= 0 {
			break
		}
		query := url.Values{
			"$count":   {"true"},
			"$orderby": {"created desc"},
			"$top":     {strconv.Itoa(pageSize)},
			"$skip":    {strconv.Itoa(skip)},
		}
		if params.CreatedAfter != "" {
			query.Set("created_after", params.CreatedAfter)
		}
		if params.CreatedBefore != "" {
			query.Set("created_before", params.CreatedBefore)
		}
		var page TestRunList
		if err := o.doV6JSON(ctx, http.MethodGet, testRunsV6Path+"?"+query.Encode(), nil, &page, http.StatusOK); err != nil {
			return nil, err
		}
		for i := range page.Value {
			normalizeTestRun(&page.Value[i])
		}
		all = append(all, page.Value...)
		if page.Count > 0 {
			total = page.Count
		}
		if len(page.Value) < pageSize || (total > 0 && skip+len(page.Value) >= total) || (params.Top > 0 && len(all) >= params.Top) {
			break
		}
		skip += len(page.Value)
	}
	return &TestRunList{Value: all, Count: total}, nil
}

func (o *cloudOperations) GetTestRun(ctx context.Context, id int) (*TestRun, error) {
	var result TestRun
	path := testRunsV6Path + "/" + strconv.Itoa(id)
	if err := o.doV6JSON(ctx, http.MethodGet, path, nil, &result, http.StatusOK); err != nil {
		return nil, err
	}
	normalizeTestRun(&result)
	return &result, nil
}

func (o *cloudOperations) GetTestRunDistribution(ctx context.Context, id int) (*TestRunDistribution, error) {
	var result TestRunDistribution
	path := fmt.Sprintf("%s/%d/distribution", testRunsV6Path, id)
	if err := o.doV6JSON(ctx, http.MethodGet, path, nil, &result, http.StatusOK); err != nil {
		return nil, err
	}
	if result.Distribution == nil {
		result.Distribution = map[string]TestRunDistributionEntry{}
	}
	return &result, nil
}

func (o *cloudOperations) DownloadTestRunScript(ctx context.Context, id int, accept string) (*ScriptDownload, error) {
	return o.downloadScript(ctx, fmt.Sprintf("%s/%d/script", testRunsV6Path, id), accept)
}

func (o *cloudOperations) downloadScript(ctx context.Context, path, accept string) (*ScriptDownload, error) {
	resp, err := o.executor.doCloud(ctx, cloudRequest{
		Target: cloudTargetCloud, Auth: cloudAuthConfigured, Method: http.MethodGet, Path: path, Accept: accept,
	})
	if err != nil {
		return nil, err
	}
	if err := checkCloudStatus(resp, "download script", http.StatusOK); err != nil {
		return nil, err
	}
	return &ScriptDownload{ContentType: resp.Header.Get("Content-Type"), Data: resp.Body}, nil
}

func (o *cloudOperations) UpdateTestRun(ctx context.Context, id int, note string) error {
	return o.doV6JSON(ctx, http.MethodPatch, testRunsV6Path+"/"+strconv.Itoa(id), map[string]string{"note": note}, nil, http.StatusNoContent)
}

func (o *cloudOperations) DeleteTestRun(ctx context.Context, id int) error {
	return o.testRunAction(ctx, id, http.MethodDelete, "")
}
func (o *cloudOperations) AbortTestRun(ctx context.Context, id int) error {
	return o.testRunAction(ctx, id, http.MethodPost, "abort")
}
func (o *cloudOperations) StarTestRun(ctx context.Context, id int) error {
	return o.testRunAction(ctx, id, http.MethodPost, "star")
}
func (o *cloudOperations) UnstarTestRun(ctx context.Context, id int) error {
	return o.testRunAction(ctx, id, http.MethodPost, "unstar")
}

func (o *cloudOperations) testRunAction(ctx context.Context, id int, method, action string) error {
	path := testRunsV6Path + "/" + strconv.Itoa(id)
	if action != "" {
		path += "/" + action
	}
	return o.doV6JSON(ctx, method, path, nil, nil, http.StatusNoContent)
}

func normalizeTestRun(run *TestRun) {
	if run.TestID == 0 {
		run.TestID = run.LoadTestID
	}
	if run.LoadTestID == 0 {
		run.LoadTestID = run.TestID
	}
	if run.ResultStatus == 0 && run.Result != nil {
		switch *run.Result {
		case "passed":
			run.ResultStatus = 1
		case "failed":
			run.ResultStatus = 2
		case "error":
			run.ResultStatus = 3
		}
	}
}
