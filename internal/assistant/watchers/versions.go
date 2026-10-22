package watchers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
	"unicode/utf8"
)

// VersionActor identifies the actor reported for a finalized definition version.
type VersionActor struct {
	Type    string `json:"type"`
	UserID  string `json:"userId,omitempty"`
	Subject string `json:"subject,omitempty"`
}

// VersionSummary describes a retained finalized definition, not runtime history.
type VersionSummary struct {
	Version                int64           `json:"version"`
	CreatedAt              time.Time       `json:"createdAt"`
	Actor                  VersionActor    `json:"actor"`
	ChangeKind             string          `json:"changeKind"`
	ChangeSummary          string          `json:"changeSummary"`
	DefinitionHash         string          `json:"definitionHash"`
	ChangedFields          []string        `json:"changedFields"`
	RollbackFromVersion    *int64          `json:"rollbackFromVersion,omitempty"`
	LearningTransition     json.RawMessage `json:"learningTransition,omitempty"`
	PreviousLearningEpoch  *int64          `json:"previousLearningEpoch,omitempty"`
	ResultingLearningEpoch *int64          `json:"resultingLearningEpoch,omitempty"`
}

// VersionDetail preserves the server's definition without applying manifest defaults.
type VersionDetail struct {
	VersionSummary

	Definition json.RawMessage `json:"definition"`
}

// VersionPage is one explicit page of retained history, not lifetime coverage.
type VersionPage struct {
	Items             []VersionSummary `json:"items"`
	NextBeforeVersion *int64           `json:"nextBeforeVersion,omitempty"`
}

// UnknownRestoreError means the request may have saved a definition. Inspect
// current status and retained history before deciding whether to retry.
type UnknownRestoreError struct {
	cause error
}

func (e *UnknownRestoreError) Error() string {
	return "restore Watcher version: outcome is uncertain; the restore may have applied; inspect current status and version history before retrying"
}

func (e *UnknownRestoreError) Unwrap() error { return e.cause }

func (c *Client) ListVersions(ctx context.Context, id string, limit int, before int64) (*VersionPage, error) {
	path, err := watcherPath(id)
	if err != nil {
		return nil, err
	}
	if limit < 1 || limit > 100 {
		return nil, errors.New("Watcher version history limit must be between 1 and 100")
	}
	if before < 0 {
		return nil, errors.New("Watcher version history cursor must be positive when supplied")
	}
	params := url.Values{"limit": {strconv.Itoa(limit)}}
	if before != 0 {
		params.Set("beforeVersion", strconv.FormatInt(before, 10))
	}
	var page struct {
		Revisions         *[]VersionSummary `json:"revisions"`
		NextBeforeVersion *int64            `json:"nextBeforeVersion"`
	}
	if err := c.read(ctx, path+"/revisions?"+params.Encode(), "list Watcher versions", false, &page); err != nil {
		return nil, versionAPIError(err)
	}
	if page.Revisions == nil {
		return nil, errors.New("list Watcher versions: response omits collection items")
	}
	items := *page.Revisions
	if len(items) > limit {
		return nil, errors.New("list Watcher versions: response exceeds the requested page size")
	}
	for i, item := range items {
		if item.Version <= 0 || (before != 0 && item.Version >= before) || (i > 0 && item.Version >= items[i-1].Version) {
			return nil, errors.New("list Watcher versions: response contains invalid version ordering")
		}
	}
	if next := page.NextBeforeVersion; next != nil {
		if *next <= 0 || len(items) == 0 || *next != items[len(items)-1].Version || (before != 0 && *next >= before) {
			return nil, errors.New("list Watcher versions: response contains an invalid pagination cursor")
		}
	}
	return &VersionPage{Items: items, NextBeforeVersion: page.NextBeforeVersion}, nil
}

func (c *Client) GetVersion(ctx context.Context, id string, version int64) (*VersionDetail, error) {
	path, err := versionPath(id, version)
	if err != nil {
		return nil, err
	}
	var item VersionDetail
	if err := c.read(ctx, path, "get Watcher version", false, &item); err != nil {
		return nil, versionAPIError(err)
	}
	if err := validateVersionDetail(&item, version); err != nil {
		return nil, fmt.Errorf("get Watcher version: %w", err)
	}
	return &item, nil
}

func (c *Client) RestoreVersion(ctx context.Context, id string, version, expected int64, summary string) (*VersionDetail, error) {
	path, err := versionPath(id, version)
	if err != nil {
		return nil, err
	}
	if expected <= 0 {
		return nil, errors.New("Watcher expected current version must be positive")
	}
	if utf8.RuneCountInString(summary) > 1000 {
		return nil, errors.New("Watcher restore summary must contain at most 1000 characters")
	}
	request := struct {
		ExpectedDefinitionVersion int64  `json:"expectedDefinitionVersion"`
		Summary                   string `json:"summary,omitempty"`
	}{expected, summary}
	var item VersionDetail
	if err := c.write(ctx, http.MethodPost, path+"/restore", "restore Watcher version", request, &item); err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode >= 400 && apiErr.StatusCode < 500 {
			return nil, versionAPIError(err)
		}
		return nil, &UnknownRestoreError{cause: err}
	}
	if err := validateVersionDetail(&item, item.Version); err != nil {
		return nil, &UnknownRestoreError{cause: err}
	}
	if item.Version <= expected || item.RollbackFromVersion == nil || *item.RollbackFromVersion != version {
		return nil, &UnknownRestoreError{cause: errors.New("response does not confirm a new version restored from the requested target")}
	}
	return &item, nil
}

func versionPath(id string, version int64) (string, error) {
	path, err := watcherPath(id)
	if err != nil {
		return "", err
	}
	if version <= 0 {
		return "", errors.New("Watcher version must be positive")
	}
	return path + "/revisions/" + strconv.FormatInt(version, 10), nil
}

func validateVersionDetail(item *VersionDetail, version int64) error {
	if item.Version <= 0 || item.Version != version {
		return errors.New("response contains an unexpected definition version")
	}
	// Schema contents are server-owned and may evolve; only require an object.
	definition := bytes.TrimSpace(item.Definition)
	if len(definition) == 0 || definition[0] != '{' || !json.Valid(definition) {
		return errors.New("response omits a valid definition object")
	}
	return nil
}

func versionAPIError(err error) error {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		switch {
		case errors.Is(err, ErrNotFound):
			apiErr.Reason = "Watcher or version is unavailable; retained versions may be pruned, or the version route may be unavailable"
		case errors.Is(err, ErrPermissionDenied):
			apiErr.Reason = "Watcher permission and access to the Watcher are required"
		case errors.Is(err, ErrConflict):
			apiErr.Reason = "Watcher version or state conflicts with restore; inspect current status and version history before retrying"
		}
	}
	return err
}
