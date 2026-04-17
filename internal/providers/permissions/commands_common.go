package permissions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/style"
)

// GrafanaConfigLoader can load a NamespacedRESTConfig from the active context.
type GrafanaConfigLoader interface {
	LoadGrafanaConfig(ctx context.Context) (config.NamespacedRESTConfig, error)
}

// readItemsFromFile reads a JSON permissions payload from path. When path is "-"
// it reads from stdin. The input may be either a bare JSON array of Items or an
// object shaped like {"items": [...]}.
func readItemsFromFile(path string, stdin io.Reader) ([]Item, error) {
	if path == "" {
		return nil, errors.New("--file is required")
	}

	var (
		data []byte
		err  error
	)
	if path == "-" {
		if stdin == nil {
			stdin = os.Stdin
		}
		data, err = io.ReadAll(stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read permissions file: %w", err)
	}

	return parseItems(data)
}

// parseItems accepts either a bare JSON array or a {"items":[…]} envelope.
func parseItems(data []byte) ([]Item, error) {
	// Try array form first.
	var items []Item
	if err := json.Unmarshal(data, &items); err == nil {
		return items, nil
	}

	var envelope setBody
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("failed to parse permissions: expected JSON array or object with 'items' field: %w", err)
	}
	return envelope.Items, nil
}

// permissionName converts a numeric permission level to its Grafana name.
func permissionName(p int) string {
	switch p {
	case PermissionView:
		return "View"
	case PermissionEdit:
		return "Edit"
	case PermissionAdmin:
		return "Admin"
	default:
		return strconv.Itoa(p)
	}
}

// PermissionsTableCodec renders a slice of permission items as a table.
type PermissionsTableCodec struct{}

// Format returns the format name.
func (c *PermissionsTableCodec) Format() format.Format { return "table" }

// Encode writes the table representation of v to w.
func (c *PermissionsTableCodec) Encode(w io.Writer, v any) error {
	items, ok := v.([]Item)
	if !ok {
		return errors.New("invalid data type for table codec: expected []Item")
	}

	t := style.NewTable("ROLE", "USER", "TEAM", "PERMISSION")
	for _, it := range items {
		team := ""
		if it.TeamID != 0 {
			team = strconv.Itoa(it.TeamID)
		}
		t.Row(it.Role, it.UserLogin, team, permissionName(it.Permission))
	}
	return t.Render(w)
}

// Decode is not supported for the table codec.
func (c *PermissionsTableCodec) Decode(r io.Reader, v any) error {
	return errors.New("table format does not support decoding")
}
