package prometheus

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

// ConvertGrafanaResponse exposes the unexported converter for the external test package.
func ConvertGrafanaResponse(grafanaResp *GrafanaQueryResponse, isRange bool) *QueryResponse {
	return convertGrafanaResponse(grafanaResp, isRange)
}
