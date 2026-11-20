package resources //nolint:testpackage // exercises unexported option setup.

import (
	"strconv"
	"testing"

	"github.com/grafana/gcx/internal/shared"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMaxConcurrentFlagDefaultsUseSharedConstant(t *testing.T) {
	setups := map[string]func(*pflag.FlagSet){
		"push":     func(f *pflag.FlagSet) { (&pushOpts{}).setup(f) },
		"delete":   func(f *pflag.FlagSet) { (&deleteOpts{}).setup(f) },
		"validate": func(f *pflag.FlagSet) { (&validateOpts{}).setup(f) },
		"serve":    func(f *pflag.FlagSet) { (&serveOpts{}).setup(f) },
	}

	for name, setup := range setups {
		t.Run(name, func(t *testing.T) {
			flags := pflag.NewFlagSet(name, pflag.ContinueOnError)
			setup(flags)

			flag := flags.Lookup("max-concurrent")
			require.NotNil(t, flag)
			assert.Equal(t, strconv.Itoa(shared.DefaultMaxConcurrency), flag.DefValue)
		})
	}
}
