package query

import (
	"errors"
	"fmt"
	"time"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/pflag"
)

// SharedOpts holds flags shared across typed query subcommands.
type SharedOpts struct {
	IO    cmdio.Options
	From  string
	To    string
	Step  string
	Since string
}

// Setup registers shared query flags on the given flag set.
func (opts *SharedOpts) Setup(flags *pflag.FlagSet) {
	RegisterCodecs(&opts.IO)
	opts.IO.BindFlags(flags)

	flags.StringVar(&opts.From, "from", "", "Start time (RFC3339, Unix timestamp, or relative like 'now-1h')")
	flags.StringVar(&opts.To, "to", "", "End time (RFC3339, Unix timestamp, or relative like 'now')")
	flags.StringVar(&opts.Step, "step", "", "Query step (e.g., '15s', '1m')")
	flags.StringVar(&opts.Since, "since", "", "Duration before --to (or now if omitted); mutually exclusive with --from")
}

// Validate validates shared flags and resolves --since into From/To.
func (opts *SharedOpts) Validate() error {
	if err := opts.IO.Validate(); err != nil {
		return err
	}

	if opts.Since == "" {
		return nil
	}

	if opts.From != "" {
		return errors.New("--since is mutually exclusive with --from")
	}

	d, err := ParseDuration(opts.Since)
	if err != nil {
		return fmt.Errorf("invalid --since duration: %w", err)
	}
	if d <= 0 {
		return errors.New("--since must be greater than 0")
	}

	now := time.Now()
	end, err := ParseTime(opts.To, now)
	if err != nil {
		return fmt.Errorf("invalid --to time: %w", err)
	}
	if end.IsZero() {
		end = now
	}
	opts.From = end.Add(-d).Format(time.RFC3339)
	opts.To = end.Format(time.RFC3339)

	return nil
}

// ParseTimes parses From/To/Step into time.Time and time.Duration values.
func (opts *SharedOpts) ParseTimes(now time.Time) (time.Time, time.Time, time.Duration, error) {
	start, err := ParseTime(opts.From, now)
	if err != nil {
		return time.Time{}, time.Time{}, 0, fmt.Errorf("invalid --from time: %w", err)
	}

	end, err := ParseTime(opts.To, now)
	if err != nil {
		return time.Time{}, time.Time{}, 0, fmt.Errorf("invalid --to time: %w", err)
	}

	step, err := ParseDuration(opts.Step)
	if err != nil {
		return time.Time{}, time.Time{}, 0, fmt.Errorf("invalid --step duration: %w", err)
	}

	return start, end, step, nil
}
