package kg

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

// SchemaList contains installed schema bundles. Bundle fields remain unstructured
// so expanded definitions retain fields added by the backend.
type SchemaList struct {
	Schemas []map[string]any `json:"schemas" yaml:"schemas"`
}

// ListSchemas fetches installed schemas, rather than types observed in the graph.
func (c *Client) ListSchemas(ctx context.Context, expand, latestOnly bool) (*SchemaList, error) {
	query := url.Values{
		"expand":      {strconv.FormatBool(expand)},
		"latest_only": {strconv.FormatBool(latestOnly)},
	}
	path := fmt.Sprintf(kgWriteAPIBase+"/schemas", url.PathEscape(c.Namespace())) + "?" + query.Encode()
	var result SchemaList
	if err := c.GetJSON(ctx, path, &result); err != nil {
		return nil, fmt.Errorf("kg: list schemas: %w", err)
	}
	return &result, nil
}
