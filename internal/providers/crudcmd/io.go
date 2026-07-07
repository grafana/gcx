package crudcmd

import (
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/pflag"
)

// SetupIO registers the given codecs, sets the default output format, and
// binds the standard -o/--json/--jq flags. This is the mechanical part every
// list/get command's opts.setup repeated verbatim.
func SetupIO(io *cmdio.Options, flags *pflag.FlagSet, defaultFormat string, codecs ...format.Codec) {
	for _, c := range codecs {
		io.RegisterCustomCodec(string(c.Format()), c)
	}
	io.DefaultFormat(defaultFormat)
	io.BindFlags(flags)
}

// DefaultLimitUsage is the flag usage string shared by every --limit flag.
const DefaultLimitUsage = "Maximum number of items to return (0 for unlimited)"

// ListOpts is the standard opts struct for list commands: IO plus a --limit
// flag with client-side truncation (see adapter.TruncateSlice).
type ListOpts struct {
	IO    cmdio.Options
	Limit int64
}

// Setup registers codecs/format/IO flags and adds a --limit flag defaulting
// to limitDefault. Pass usage == "" to use DefaultLimitUsage.
func (o *ListOpts) Setup(flags *pflag.FlagSet, defaultFormat string, limitDefault int64, usage string, codecs ...format.Codec) {
	SetupIO(&o.IO, flags, defaultFormat, codecs...)
	if usage == "" {
		usage = DefaultLimitUsage
	}
	flags.Int64Var(&o.Limit, "limit", limitDefault, usage)
}

// GetOpts is the standard opts struct for get commands: just IO, no limit.
type GetOpts struct {
	IO cmdio.Options
}

// Setup registers codecs/format/IO flags.
func (o *GetOpts) Setup(flags *pflag.FlagSet, defaultFormat string, codecs ...format.Codec) {
	SetupIO(&o.IO, flags, defaultFormat, codecs...)
}

// MutateOpts is the standard opts struct for file-based create/update
// commands: IO plus a -f/--filename flag.
type MutateOpts struct {
	IO   cmdio.Options
	File string
}

// Setup registers codecs/format/IO flags and the -f/--filename flag with the
// given usage string.
func (o *MutateOpts) Setup(flags *pflag.FlagSet, defaultFormat string, filenameUsage string, codecs ...format.Codec) {
	SetupIO(&o.IO, flags, defaultFormat, codecs...)
	flags.StringVarP(&o.File, "filename", "f", "", filenameUsage)
}
