package cloud

import "net/http"

// HTTPClientForTest exposes the transport and default timeout for deadline tests.
func HTTPClientForTest(c *GCOMClient) *http.Client { return c.http }
