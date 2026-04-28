package alert

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/grafana/gcx/internal/config"
	cmdio "github.com/grafana/gcx/internal/output"
	"sigs.k8s.io/yaml"
)

// readProvisioningInput decodes a plain JSON/YAML object from a file path or
// stdin (when file == "-") into the provided target. Unlike the K8s-envelope
// readers used elsewhere, provisioning resources are posted as bare objects
// and therefore have no apiVersion/kind/spec wrapper.
func readProvisioningInput(file string, stdin io.Reader, out any) error {
	if file == "" {
		return errors.New("--filename is required (use - to read from stdin)")
	}

	var reader io.Reader
	if file == "-" {
		reader = stdin
	} else {
		f, err := os.Open(file)
		if err != nil {
			return fmt.Errorf("failed to open file %s: %w", file, err)
		}
		defer f.Close()
		reader = f
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("failed to read input: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return errors.New("input is empty")
	}

	// sigs.k8s.io/yaml decodes both JSON and YAML into JSON, then unmarshals.
	if err := yaml.Unmarshal(data, out); err != nil {
		return fmt.Errorf("failed to parse input: %w", err)
	}
	return nil
}

// confirmDestructive prompts the user to confirm a destructive operation,
// honoring the --force flag and the GCX_AUTO_APPROVE env var. Returns true if
// the caller should proceed, false if the user declined.
func confirmDestructive(in io.Reader, out io.Writer, force bool, prompt string) (bool, error) {
	if force {
		return true, nil
	}
	cliOpts, err := config.LoadCLIOptions()
	if err != nil {
		return false, err
	}
	if cliOpts.AutoApprove {
		return true, nil
	}

	fmt.Fprintf(out, "%s [y/N] ", prompt)
	answer, err := bufio.NewReader(in).ReadString('\n')
	if err != nil {
		return false, fmt.Errorf("failed to read confirmation: %w", err)
	}
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer != "y" && answer != "yes" {
		cmdio.Info(out, "Aborted.")
		return false, nil
	}
	return true, nil
}

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
