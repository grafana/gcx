package azure

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/grafana/gcx/internal/docs"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/onboard"
)

// onboardTextCodec renders an onboard.Result as a human-friendly summary.
type onboardTextCodec struct{}

func (c *onboardTextCodec) Format() format.Format { return "text" }

func (c *onboardTextCodec) Encode(w io.Writer, value any) error {
	res, ok := value.(onboard.Result)
	if !ok {
		return fmt.Errorf("onboard text codec: unsupported type %T", value)
	}

	if len(res.Cleaned) > 0 || (res.DryRun && len(res.Datasources) == 0) {
		return c.encodeCleaned(w, res)
	}

	if len(res.Datasources) == 0 {
		fmt.Fprintln(w, "No datasources were created.")
		return nil
	}

	if res.DryRun {
		fmt.Fprintf(w, "Dry run — %d datasource(s) planned (nothing was created):\n", len(res.Datasources))
	} else {
		fmt.Fprintf(w, "%d datasource(s):\n", len(res.Datasources))
	}
	for _, d := range res.Datasources {
		c.encodeDatasource(w, d)
	}
	return nil
}

func (c *onboardTextCodec) encodeCleaned(w io.Writer, res onboard.Result) error {
	verb := "Removed"
	if res.DryRun {
		verb = "Would remove"
	}
	if len(res.Cleaned) == 0 {
		fmt.Fprintf(w, "%s 0 gcx-created artifact(s).\n", verb)
		return nil
	}
	fmt.Fprintf(w, "%s %d gcx-created artifact(s):\n", verb, len(res.Cleaned))
	for _, cl := range res.Cleaned {
		fmt.Fprintf(w, "  - %s %s\n", cl.Kind, cl.Name)
	}
	return nil
}

func (c *onboardTextCodec) encodeDatasource(w io.Writer, d onboard.DatasourceResult) {
	fmt.Fprintf(w, "  - %s (%s)", d.Name, d.Type)
	if d.UID != "" {
		fmt.Fprintf(w, " uid=%s", d.UID)
	}
	if d.Credential != nil && d.Credential.ID != "" {
		fmt.Fprintf(w, " id=%s", d.Credential.ID)
	}
	if d.Status != "" {
		fmt.Fprintf(w, " [%s]", d.Status)
	}
	if d.Health != "" {
		fmt.Fprintf(w, " health=%s", d.Health)
	}
	if d.Credential != nil && len(d.Credential.Roles) > 0 {
		fmt.Fprintf(w, " roles=%s", strings.Join(d.Credential.Roles, ","))
	}
	if d.Note != "" {
		fmt.Fprintf(w, " (%s)", d.Note)
	}
	fmt.Fprintln(w)
	if d.HealthMessage != "" {
		fmt.Fprintf(w, "      health: %s\n", d.HealthMessage)
	}
	if d.Hint != "" {
		fmt.Fprintf(w, "      hint: %s\n", d.Hint)
		if d.HintDocs != "" {
			fmt.Fprintf(w, "      docs: %s\n", docs.HumanURL(d.HintDocs))
		}
	}
}

func (c *onboardTextCodec) Decode(io.Reader, any) error {
	return errors.New("onboard text codec does not support decoding")
}
