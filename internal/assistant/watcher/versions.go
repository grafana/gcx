package watcher

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"

	"github.com/grafana/gcx/internal/assistant/watchers"
)

// DefinitionChange uses JSON pointers. A nil value means the field is absent;
// a pointer to JSON null means the field is present with a null value.
type DefinitionChange struct {
	Path      string           `json:"path"`
	Operation string           `json:"operation"`
	Before    *json.RawMessage `json:"before,omitempty"`
	After     *json.RawMessage `json:"after,omitempty"`
}

// DiffDefinitions compares retained definitions without applying manifest
// defaults. Object keys are sorted, arrays are atomic, and numbers retain their
// JSON representation rather than being rounded through floating point.
func DiffDefinitions(from, to json.RawMessage) ([]DefinitionChange, error) {
	before, err := decodeDefinition(from)
	if err != nil {
		return nil, fmt.Errorf("decode source definition: %w", err)
	}
	after, err := decodeDefinition(to)
	if err != nil {
		return nil, fmt.Errorf("decode target definition: %w", err)
	}
	changes := make([]DefinitionChange, 0)
	diffDefinitionValue("", before, after, &changes)
	slices.SortFunc(changes, func(a, b DefinitionChange) int { return strings.Compare(a.Path, b.Path) })
	return changes, nil
}

func decodeDefinition(raw json.RawMessage) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil {
		return nil, errors.New("invalid JSON")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("expected one JSON value")
	}
	return value, nil
}

func diffDefinitionValue(path string, before, after any, changes *[]DefinitionChange) {
	if reflect.DeepEqual(before, after) {
		return
	}
	from, fromObject := before.(map[string]any)
	to, toObject := after.(map[string]any)
	if !fromObject || !toObject {
		*changes = append(*changes, DefinitionChange{Path: path, Operation: "replace", Before: definitionJSON(before), After: definitionJSON(after)})
		return
	}
	keys := make([]string, 0, len(from)+len(to))
	for key := range from {
		keys = append(keys, key)
	}
	for key := range to {
		if _, exists := from[key]; !exists {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	for _, key := range keys {
		child := path + "/" + strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
		old, oldExists := from[key]
		next, nextExists := to[key]
		switch {
		case !oldExists:
			*changes = append(*changes, DefinitionChange{Path: child, Operation: "add", After: definitionJSON(next)})
		case !nextExists:
			*changes = append(*changes, DefinitionChange{Path: child, Operation: "remove", Before: definitionJSON(old)})
		default:
			diffDefinitionValue(child, old, next, changes)
		}
	}
}

func definitionJSON(value any) *json.RawMessage {
	// Values come only from the JSON decoder, so marshaling cannot fail.
	raw, _ := json.Marshal(value) //nolint:errchkjson // The JSON decoder produces only marshalable values, including json.Number.
	message := json.RawMessage(raw)
	return &message
}

// VersionRestoreObservation is a subsequent read, not the restore's commit state.
type VersionRestoreObservation struct {
	CurrentVersion int64  `json:"currentVersion"`
	Lifecycle      string `json:"lifecycle"`
	Archived       bool   `json:"archived"`
}

// VersionRestoreOutcome separates acceptance evidence from later observations.
// Unknown writes deliberately omit Changed and Accepted.
type VersionRestoreOutcome struct {
	Outcome  string                     `json:"outcome"`
	Changed  *bool                      `json:"changed,omitempty"`
	Accepted *watchers.VersionDetail    `json:"accepted,omitempty"`
	Observed *VersionRestoreObservation `json:"observed,omitempty"`
	Guidance string                     `json:"guidance,omitempty"`
}

// ValidateVersionRestore checks the resolved snapshot and guard before the
// caller asks for confirmation. The server remains authoritative across races.
func ValidateVersionRestore(current *watchers.Watcher, expected int64) error {
	if current == nil || strings.TrimSpace(current.ID) == "" {
		return errors.New("restore Watcher version requires a resolved Watcher identity")
	}
	if current.ArchivedAt != nil {
		return errors.New("restore Watcher version requires a non-archived Watcher")
	}
	if !inactiveForVersionRestore(current.Status) {
		return errors.New("restore Watcher version requires an inactive Watcher (draft, paused, or ready); inspect gcx assistant watchers status WATCHER")
	}
	if current.DefinitionVersion != expected {
		return fmt.Errorf("restore Watcher version: expected current version %d does not match observed version %d; inspect current status before choosing a new guard", expected, current.DefinitionVersion)
	}
	return nil
}

func inactiveForVersionRestore(status string) bool {
	return status == "draft" || status == "paused" || status == "ready"
}

// RestoreVersion submits one guarded restore after the caller validates the
// resolved snapshot and confirms intent. It never changes lifecycle, calibrates,
// or retries a write.
// An outcome accompanied by an error must still be shown to the caller.
func RestoreVersion(ctx context.Context, client *watchers.Client, current *watchers.Watcher, version, expected int64, summary string) (*VersionRestoreOutcome, error) {
	if client == nil {
		return nil, errors.New("restore Watcher version requires a client")
	}
	if current == nil || strings.TrimSpace(current.ID) == "" {
		return nil, errors.New("restore Watcher version requires a resolved Watcher identity")
	}
	result := &VersionRestoreOutcome{}
	accepted, err := client.RestoreVersion(ctx, current.ID, version, expected, summary)
	if err != nil {
		var unknown *watchers.UnknownRestoreError
		if errors.As(err, &unknown) {
			// A cancelled response decoder can report only an invalid body.
			// Keep cancellation discoverable without losing the uncertain write.
			result.Outcome = "unknown"
			result.Guidance = "Restore may have applied. Inspect Watcher status and retained version history before retrying; do not resubmit blindly."
			return result, errors.Join(err, ctx.Err())
		}
		var apiErr *watchers.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode >= 400 && apiErr.StatusCode < 500 {
			changed := false
			result.Outcome = "rejected"
			result.Changed = &changed
			result.Guidance = "Restore was rejected. Inspect Watcher status and retained version history before retrying. No automatic retry was attempted."
			if observed, readErr := client.Get(ctx, current.ID); readErr == nil {
				result.Observed = &VersionRestoreObservation{CurrentVersion: observed.DefinitionVersion, Lifecycle: observed.Status, Archived: observed.ArchivedAt != nil}
			}
			return result, err
		}
		return nil, err
	}
	changed := true
	result.Accepted = accepted
	result.Changed = &changed
	observed, err := client.Get(ctx, current.ID)
	if err != nil {
		result.Outcome = "saved-unverified"
		result.Guidance = "Restore saved a new version, but current state could not be verified. Inspect Watcher status before taking further action; do not repeat the restore."
		return result, fmt.Errorf("Watcher version restore saved, but verification failed: %w", err)
	}
	result.Observed = &VersionRestoreObservation{CurrentVersion: observed.DefinitionVersion, Lifecycle: observed.Status, Archived: observed.ArchivedAt != nil}
	if observed.DefinitionVersion != accepted.Version || observed.ArchivedAt != nil || !inactiveForVersionRestore(observed.Status) {
		result.Outcome = "saved-unverified"
		result.Guidance = "Restore saved a new version, but the later Watcher state does not confirm that version is current and inactive. Inspect Watcher status; do not repeat the restore."
		return result, errors.New("Watcher version restore saved, but later state does not match verification")
	}
	result.Outcome = "accepted"
	return result, nil
}
