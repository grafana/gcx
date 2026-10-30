package query

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// SQLQueryOpts adds file input to SQL commands without exposing it on other
// query languages that use SharedOpts.
type SQLQueryOpts struct {
	SharedOpts

	Filename string
}

func (opts *SQLQueryOpts) Setup(flags *pflag.FlagSet, enableGraph bool) {
	opts.SharedOpts.Setup(flags, enableGraph)
	flags.StringVarP(&opts.Filename, "filename", "f", "", "Read SQL verbatim from FILE (- for stdin); disables the default row limit and cannot be combined with --limit")
}

// ResolveExpr validates and reads file input before config or datasource I/O.
func (opts *SQLQueryOpts) ResolveExpr(cmd *cobra.Command, args []string, exprArgIndex int) (string, error) {
	if !cmd.Flags().Changed("filename") {
		expr, err := opts.SharedOpts.ResolveExpr(args, exprArgIndex)
		if err != nil {
			return "", gcxerrors.NewCommandUsageError(cmd, err.Error(), err)
		}
		return expr, nil
	}
	if cmd.Flags().Changed("limit") {
		return "", gcxerrors.NewCommandUsageError(cmd, "--filename cannot be combined with --limit", nil)
	}
	if cmd.Flags().Changed("expr") || exprArgIndex < len(args) {
		return "", gcxerrors.NewCommandUsageError(cmd, "provide SQL as a positional argument, via --expr, or via --filename, not multiple sources", nil)
	}
	if opts.Filename == "" {
		return "", gcxerrors.NewCommandUsageError(cmd, "--filename requires a file path (use - for stdin)", nil)
	}

	var data []byte
	var err error
	if opts.Filename == "-" {
		cmdio.Info(cmd.ErrOrStderr(), "Reading SQL from stdin until EOF")
		data, err = io.ReadAll(cmd.InOrStdin())
	} else {
		data, err = os.ReadFile(opts.Filename)
	}
	if err != nil {
		return "", gcxerrors.NewCommandUsageError(cmd, fmt.Sprintf("failed to read --filename %q", opts.Filename), err)
	}
	if strings.TrimSpace(string(data)) == "" {
		return "", gcxerrors.NewCommandUsageError(cmd, fmt.Sprintf("--filename %q is empty", opts.Filename), nil)
	}
	cmdio.Warning(cmd.ErrOrStderr(), "SQL read from --filename runs without the default row limit; set a row limit in the SQL if needed")
	return string(data), nil
}

// RowLimit disables SQL rewriting for file input, preserving scripts, comments,
// and line endings. Inline queries retain their established limit behavior.
func (opts *SQLQueryOpts) RowLimit(limit int) int {
	if opts.Filename != "" {
		return 0
	}
	return limit
}
