package testutils

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

type CommandAssertion func(*testing.T, CommandResult)

func CommandSuccess() CommandAssertion {
	return func(t *testing.T, result CommandResult) {
		t.Helper()

		require.NoError(t, result.Err)
	}
}

func CommandErrorContains(message string) CommandAssertion {
	return func(t *testing.T, result CommandResult) {
		t.Helper()

		require.Error(t, result.Err)
		require.ErrorContains(t, result.Err, message)
	}
}

func CommandOutputContains(expected string) CommandAssertion {
	return func(t *testing.T, result CommandResult) {
		t.Helper()

		require.Contains(t, result.Stdout, expected)
	}
}

func CommandOutputDoesNotContain(unexpected string) CommandAssertion {
	return func(t *testing.T, result CommandResult) {
		t.Helper()

		require.NotContains(t, result.Stdout, unexpected)
	}
}

func CommandOutputEquals(expected string) CommandAssertion {
	return func(t *testing.T, result CommandResult) {
		t.Helper()

		require.Equal(t, expected, result.Stdout)
	}
}

type CommandResult struct {
	Err    error
	Stdout string
}

type CommandTestCase struct {
	Cmd     *cobra.Command
	Command []string

	Assertions []CommandAssertion

	Stdin io.Reader
	Env   map[string]string
}

func (testCase CommandTestCase) Run(t *testing.T) {
	t.Helper()

	var stdin io.Reader = &bytes.Buffer{}
	if testCase.Stdin != nil {
		stdin = testCase.Stdin
	}
	stdout := &bytes.Buffer{}

	// To avoid polluting the tests output
	testCase.Cmd.SilenceErrors = true

	testCase.Cmd.SetIn(stdin)
	testCase.Cmd.SetOut(stdout)
	testCase.Cmd.SetArgs(testCase.Command)

	env := testCase.SetEnv()
	err := testCase.Cmd.Execute()
	testCase.RestoreEnv(env)

	result := CommandResult{
		Err:    err,
		Stdout: stdout.String(),
	}

	for _, assertion := range testCase.Assertions {
		assertion(t, result)
	}
}

func (testCase CommandTestCase) SetEnv() map[string]string {
	env := os.Environ()
	os.Clearenv()

	res := make(map[string]string, len(env))
	for _, e := range env {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) != 2 {
			panic("invalid env KV pair: " + e)
		}
		res[parts[0]] = parts[1]
	}

	for k, v := range testCase.Env {
		os.Setenv(k, v)
	}

	return res
}

func (testCase CommandTestCase) RestoreEnv(env map[string]string) {
	os.Clearenv()

	for k, v := range env {
		os.Setenv(k, v)
	}
}
