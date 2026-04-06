package pyroscope

// Test helpers — expose internal path builders for external test package.

func (c *Client) BuildResourcePath(datasourceUID, resourcePath string) string {
	return c.buildResourcePath(datasourceUID, resourcePath)
}
