package prometheus

import "io"

// Test helpers — expose internal path builders for external test package.

func (c *Client) BuildLabelsPath(datasourceUID string) string {
	return c.buildLabelsPath(datasourceUID)
}

func (c *Client) BuildLabelValuesPath(datasourceUID, labelName string) string {
	return c.buildLabelValuesPath(datasourceUID, labelName)
}

func (c *Client) BuildMetadataPath(datasourceUID string) string {
	return c.buildMetadataPath(datasourceUID)
}

func (c *Client) BuildSeriesPath(datasourceUID string) string {
	return c.buildSeriesPath(datasourceUID)
}

func (c *Client) BuildCardinalityLabelNamesPath(datasourceUID string) string {
	return c.buildCardinalityLabelNamesPath(datasourceUID)
}

func (c *Client) BuildCardinalityLabelValuesPath(datasourceUID string) string {
	return c.buildCardinalityLabelValuesPath(datasourceUID)
}

func (c *Client) BuildSearchMetricNamesPath(datasourceUID string) string {
	return c.buildSearchMetricNamesPath(datasourceUID)
}

func (c *Client) BuildSearchLabelNamesPath(datasourceUID string) string {
	return c.buildSearchLabelNamesPath(datasourceUID)
}

func (c *Client) BuildSearchLabelValuesPath(datasourceUID string) string {
	return c.buildSearchLabelValuesPath(datasourceUID)
}

// ConvertGrafanaResponse exposes the unexported converter for the external test package.
func ConvertGrafanaResponse(grafanaResp *GrafanaQueryResponse, isRange bool) *QueryResponse {
	return convertGrafanaResponse(grafanaResp, isRange)
}

// DecodeSearchStream exposes the unexported NDJSON search decoder with a
// caller-chosen size cap, returning the result count.
func DecodeSearchStream(body io.Reader, limit int64) (int, bool, []string, error) {
	results, hasMore, warnings, err := decodeSearchStream(body, limit)
	return len(results), hasMore, warnings, err
}
