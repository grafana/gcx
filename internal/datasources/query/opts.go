package query

import (
	"errors"
	"fmt"
	"time"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/pflag"
)

// TimeRangeOpts holds --from, --to, and --since flags for time range resolution.
// It can be embedded by any command that needs time range support without the full
// SharedOpts (e.g., traces get which has no Step or shared IO).
type TimeRangeOpts struct {
	From  string
	To    string
	Since string
}

// SetupTimeFlags registers --from, --to, and --since flags on the given flag set.
func (opts *TimeRangeOpts) SetupTimeFlags(flags *pflag.FlagSet) {
	flags.StringVar(&opts.From, "from", "", "Start time (RFC3339, Unix timestamp, or relative like 'now-1h')")
	flags.StringVar(&opts.To, "to", "", "End time (RFC3339, Unix timestamp, or relative like 'now')")
	flags.StringVar(&opts.Since, "since", "", "Duration before --to (or now if omitted); mutually exclusive with --from")
}

// ValidateTimeRange validates --from/--to pairing and resolves --since into From/To.
func (opts *TimeRangeOpts) ValidateTimeRange() error {
	// Validate --from/--to pairing when --since is not used.
	if opts.Since == "" {
		if opts.From != "" && opts.To == "" {
			return errors.New("--to is required when --from is set")
		}
		if opts.To != "" && opts.From == "" {
			return errors.New("--from is required when --to is set")
		}
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

// IsRange returns true when both From and To are set, indicating a range query.
// It should be called after ValidateTimeRange() which resolves --since into From/To.
func (opts *TimeRangeOpts) IsRange() bool {
	return opts.From != "" && opts.To != ""
}

// ParseTimeRange parses From/To into time.Time values.
func (opts *TimeRangeOpts) ParseTimeRange(now time.Time) (time.Time, time.Time, error) {
	start, err := ParseTime(opts.From, now)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid --from time: %w", err)
	}

	end, err := ParseTime(opts.To, now)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid --to time: %w", err)
	}

	return start, end, nil
}

// SharedOpts holds flags shared across typed query subcommands.
type SharedOpts struct {
	TimeRangeOpts

	IO   cmdio.Options
	Step string
}

// Setup registers shared query flags on the given flag set.
func (opts *SharedOpts) Setup(flags *pflag.FlagSet) {
	RegisterCodecs(&opts.IO)
	opts.IO.BindFlags(flags)

	opts.SetupTimeFlags(flags)
	flags.StringVar(&opts.Step, "step", "", "Query step (e.g., '15s', '1m')")
}

// Validate validates shared flags and resolves --since into From/To.
func (opts *SharedOpts) Validate() error {
	if err := opts.IO.Validate(); err != nil {
		return err
	}
	return opts.ValidateTimeRange()
}

// ParseTimes parses From/To/Step into time.Time and time.Duration values.
func (opts *SharedOpts) ParseTimes(now time.Time) (time.Time, time.Time, time.Duration, error) {
	start, end, err := opts.ParseTimeRange(now)
	if err != nil {
		return time.Time{}, time.Time{}, 0, err
	}

	step, err := ParseDuration(opts.Step)
	if err != nil {
		return time.Time{}, time.Time{}, 0, fmt.Errorf("invalid --step duration: %w", err)
	}

	return start, end, step, nil
}
