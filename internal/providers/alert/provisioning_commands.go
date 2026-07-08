package alert

import (
	"fmt"
)

// validateExportFormat rejects unknown export formats early with a helpful
// error. Grafana's provisioning export endpoint supports yaml, json and hcl.
func validateExportFormat(format string) error {
	switch format {
	case "yaml", "json", "hcl":
		return nil
	default:
		return fmt.Errorf("invalid export format %q: must be yaml, json, or hcl", format)
	}
}
