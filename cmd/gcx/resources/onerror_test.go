package resources_test

import (
	"testing"

	"github.com/grafana/gcx/cmd/gcx/resources"
	"github.com/stretchr/testify/require"
)

func TestOnErrorMode_StopOnError(t *testing.T) {
	tests := []struct {
		mode resources.OnErrorMode
		want bool
	}{
		{resources.OnErrorIgnore, false},
		{resources.OnErrorFail, false},
		{resources.OnErrorAbort, true},
	}

	for _, tc := range tests {
		t.Run(string(tc.mode), func(t *testing.T) {
			require.Equal(t, tc.want, tc.mode.StopOnError())
		})
	}
}

func TestOnErrorMode_FailOnErrors(t *testing.T) {
	tests := []struct {
		mode resources.OnErrorMode
		want bool
	}{
		{resources.OnErrorIgnore, false},
		{resources.OnErrorFail, true},
		{resources.OnErrorAbort, true},
	}

	for _, tc := range tests {
		t.Run(string(tc.mode), func(t *testing.T) {
			require.Equal(t, tc.want, tc.mode.FailOnErrors())
		})
	}
}

func TestOnErrorMode_Validate(t *testing.T) {
	tests := []struct {
		mode    resources.OnErrorMode
		wantErr bool
	}{
		{resources.OnErrorIgnore, false},
		{resources.OnErrorFail, false},
		{resources.OnErrorAbort, false},
		{"bad-value", true},
		{"", true},
	}

	for _, tc := range tests {
		t.Run(string(tc.mode), func(t *testing.T) {
			err := tc.mode.Validate()
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
