package query

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/pflag"
)

// SQLQueryOpts holds flags and expression resolution shared by typed SQL
// datasource commands. It deliberately wraps SharedOpts instead of adding the
// --query-file flag to SharedOpts.Setup, so non-SQL query commands do not
// advertise an input mode they do not support.
type SQLQueryOpts struct {
	SharedOpts
	QueryFile string
}

// Setup registers the common query flags and the SQL-only --query-file flag.
func (opts *SQLQueryOpts) Setup(flags *pflag.FlagSet, enableGraph bool) {
	opts.SharedOpts.Setup(flags, enableGraph)
	flags.StringVar(&opts.QueryFile, "query-file", "", "Read the SQL query from FILE (use - for stdin)")
}

// ResolveExpr resolves a SQL expression from exactly one of a positional
// argument, --expr, or --query-file. queryFileChanged must be supplied by the
// command so an explicitly empty --query-file value cannot silently fall back
// to another source.
func (opts *SQLQueryOpts) ResolveExpr(args []string, exprArgIndex int, stdin io.Reader, queryFileChanged bool) (string, error) {
	haveFlag := opts.Expr != ""
	haveArg := exprArgIndex < len(args)
	haveFile := queryFileChanged || opts.QueryFile != ""
	if !haveFile {
		// Keep the established positional/--expr contract and its error text
		// unchanged when --query-file is not in use.
		return opts.SharedOpts.ResolveExpr(args, exprArgIndex)
	}

	sources := 0
	if haveFlag {
		sources++
	}
	if haveArg {
		sources++
	}
	if haveFile {
		sources++
	}
	if sources > 1 {
		return "", errors.New("provide the SQL query as a positional argument, via --expr, or via --query-file, not multiple sources")
	}
	if sources == 0 {
		return "", errors.New("SQL query is required: provide it as a positional argument, via --expr, or via --query-file")
	}

	if haveFlag {
		return opts.Expr, nil
	}
	if haveArg {
		return args[exprArgIndex], nil
	}
	if opts.QueryFile == "" {
		return "", errors.New("--query-file requires a file path (use - to read from stdin)")
	}

	data, err := readSQLQueryFile(opts.QueryFile, stdin)
	if err != nil {
		return "", fmt.Errorf("failed to read --query-file %q: %w", opts.QueryFile, err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return "", fmt.Errorf("--query-file %q is empty", opts.QueryFile)
	}

	// Keep the file bytes unchanged. In particular, do not trim a trailing
	// newline or normalize whitespace before the datasource receives the SQL.
	return string(data), nil
}

func readSQLQueryFile(path string, stdin io.Reader) ([]byte, error) {
	if path == "-" {
		if stdin == nil {
			stdin = os.Stdin
		}
		return io.ReadAll(stdin)
	}
	return os.ReadFile(path)
}
