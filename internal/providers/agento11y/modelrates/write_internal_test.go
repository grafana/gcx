package modelrates

import (
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newCreateFlags wires a createOpts to a flag set the way the command does, so these
// tests exercise the same flags.Changed logic the CLI relies on.
func newCreateFlags(t *testing.T, args ...string) (*createOpts, *pflag.FlagSet) {
	t.Helper()
	opts := &createOpts{}
	flags := pflag.NewFlagSet("create", pflag.ContinueOnError)
	opts.setup(flags)
	require.NoError(t, flags.Parse(args))
	return opts, flags
}

// TestToWriteDistinguishesUnsetFromZero is the property the whole flag layer
// rests on. A rate nobody mentioned must not be sent, because the server reads
// an absent rate as "nothing configured for this bucket" and a zero as "this
// model does not charge for it" — two different statements about a contract.
func TestToWriteDistinguishesUnsetFromZero(t *testing.T) {
	opts, flags := newCreateFlags(t,
		"--provider", "openai", "--model", "gpt-5.5",
		"--price-input", "2.00",
		"--price-cache-read", "0",
	)
	write, err := opts.toWrite(flags)
	require.NoError(t, err)

	require.NotNil(t, write.InputUSDPerMillion)
	assert.InDelta(t, 2.0, *write.InputUSDPerMillion, 0)

	// Passed as zero: sent as zero.
	require.NotNil(t, write.CacheReadUSDPerMillion, "an explicit zero must reach the server")
	assert.InDelta(t, 0.0, *write.CacheReadUSDPerMillion, 0)

	// Never mentioned: absent.
	assert.Nil(t, write.OutputUSDPerMillion)
	assert.Nil(t, write.RequestUSD)
	assert.Nil(t, write.CacheWriteUSDPerMillion)
	assert.Nil(t, write.LongContext)
}

func TestToWriteRejectsIncompleteInput(t *testing.T) {
	cases := []struct {
		name   string
		args   []string
		wantIn string
	}{
		{
			name:   "no provider",
			args:   []string{"--model", "gpt-5.5", "--price-input", "2"},
			wantIn: "--provider is required",
		},
		{
			name:   "no model",
			args:   []string{"--provider", "openai", "--price-input", "2"},
			wantIn: "--model is required",
		},
		{
			// A row is the whole price for a model, so one that prices nothing
			// would silently mean the model is free for this tenant.
			name:   "prices nothing at all",
			args:   []string{"--provider", "openai", "--model", "gpt-5.5"},
			wantIn: "at least one of --price-input",
		},
		{
			// A tier without a threshold has no point at which it starts.
			name:   "tier rates with no threshold",
			args:   []string{"--provider", "openai", "--model", "gpt-5.5", "--price-input", "2", "--long-context-price-input", "4"},
			wantIn: "--long-context-threshold is required",
		},
		{
			name:   "a threshold that is not positive",
			args:   []string{"--provider", "openai", "--model", "gpt-5.5", "--price-input", "2", "--long-context-threshold", "0"},
			wantIn: "--long-context-threshold is required",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts, flags := newCreateFlags(t, tc.args...)
			_, err := opts.toWrite(flags)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantIn)
		})
	}
}

func TestToWriteCarriesTheLongContextTier(t *testing.T) {
	opts, flags := newCreateFlags(t,
		"--provider", " OpenAI ", "--model", " gpt-5.5 ",
		"--price-input", "2.00", "--price-output", "8.00",
		"--long-context-threshold", "272000",
		"--long-context-price-input", "4.00",
	)
	write, err := opts.toWrite(flags)
	require.NoError(t, err)

	// Trimmed, because a stray space in a shell argument is not a different
	// provider.
	assert.Equal(t, "OpenAI", write.Provider)
	assert.Equal(t, "gpt-5.5", write.Model)

	require.NotNil(t, write.LongContext)
	assert.Equal(t, int64(272000), write.LongContext.ThresholdInputTokens)
	require.NotNil(t, write.LongContext.InputUSDPerMillion)
	assert.InDelta(t, 4.0, *write.LongContext.InputUSDPerMillion, 0)
	// A tier rate left unset falls back to this row's own base rate, so it is
	// absent rather than a copy of it.
	assert.Nil(t, write.LongContext.OutputUSDPerMillion)
}

// TestPerMillionDistinguishesUnsetFromZero is the same distinction on the way
// out: a dash where nothing is configured, a number where zero is.
func TestPerMillionDistinguishesUnsetFromZero(t *testing.T) {
	assert.Equal(t, "-", perMillion(nil))
	assert.Equal(t, "0", perMillion(new(float64(0))))
	assert.Equal(t, "2", perMillion(new(float64(2))))
	assert.Equal(t, "0.5", perMillion(new(float64(0.5))))
}
