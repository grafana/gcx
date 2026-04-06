package loki

// Test helpers — expose internal path builders for external test package.

func (c *Client) BuildLabelsPath(datasourceUID string) string {
	return c.buildLabelsPath(datasourceUID)
}

func (c *Client) BuildLabelValuesPath(datasourceUID, labelName string) string {
	return c.buildLabelValuesPath(datasourceUID, labelName)
}

func (c *Client) BuildSeriesPath(datasourceUID string) string {
	return c.buildSeriesPath(datasourceUID)
}
