package config_test

import (
	"errors"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/credentials"
	"github.com/stretchr/testify/require"
)

type persistenceProbeStore struct {
	err    error
	values map[string]string
}

func (s *persistenceProbeStore) Get(key string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	value, ok := s.values[key]
	if !ok {
		return "", credentials.ErrNotFound
	}
	return value, nil
}

func (s *persistenceProbeStore) Set(key, value string) error {
	if s.err != nil {
		return s.err
	}
	s.values[key] = value
	return nil
}

func (s *persistenceProbeStore) Delete(key string) error {
	if s.err != nil {
		return s.err
	}
	delete(s.values, key)
	return nil
}

func TestCheckOAuthCredentialPersistence(t *testing.T) {
	tests := []struct {
		name    string
		store   credentials.Store
		wantErr error
	}{
		{
			name:  "writable store",
			store: &persistenceProbeStore{values: map[string]string{}},
		},
		{
			name:  "unavailable store keeps plaintext fallback",
			store: &persistenceProbeStore{err: credentials.ErrUnavailable},
		},
		{
			name:    "restricted session fails closed",
			store:   &persistenceProbeStore{err: credentials.ErrRestrictedSession},
			wantErr: credentials.ErrRestrictedSession,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			restore := config.SetKeychainStoreFnForTest(func() credentials.Store { return tt.store })
			t.Cleanup(restore)

			err := config.CheckOAuthCredentialPersistence()
			if tt.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestCheckOAuthCredentialPersistenceKeepsUnknownFailuresFatal(t *testing.T) {
	want := errors.New("unknown store failure")
	restore := config.SetKeychainStoreFnForTest(func() credentials.Store {
		return &persistenceProbeStore{err: want}
	})
	t.Cleanup(restore)

	require.ErrorIs(t, config.CheckOAuthCredentialPersistence(), want)
}
