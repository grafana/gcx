package credentials_test

import (
	"errors"
	"testing"

	"github.com/grafana/gcx/internal/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type probeStore struct {
	values      map[string]string
	setErr      error
	getErr      error
	deleteErr   error
	setCalls    int
	getCalls    int
	deleteCalls int
}

func (s *probeStore) Get(key string) (string, error) {
	s.getCalls++
	if s.getErr != nil {
		return "", s.getErr
	}
	value, ok := s.values[key]
	if !ok {
		return "", credentials.ErrNotFound
	}
	return value, nil
}

func (s *probeStore) Set(key, value string) error {
	s.setCalls++
	if s.setErr != nil {
		return s.setErr
	}
	s.values[key] = value
	return nil
}

func (s *probeStore) Delete(key string) error {
	s.deleteCalls++
	if s.deleteErr != nil {
		return s.deleteErr
	}
	delete(s.values, key)
	return nil
}

func TestCheckWritableWritesReadsAndRemovesProbe(t *testing.T) {
	store := &probeStore{values: map[string]string{}}

	require.NoError(t, credentials.CheckWritable(store))
	assert.Equal(t, 1, store.setCalls)
	assert.Equal(t, 1, store.getCalls)
	assert.Equal(t, 1, store.deleteCalls)
	assert.Empty(t, store.values)
}

func TestCheckWritableStopsAfterWriteFailure(t *testing.T) {
	want := errors.New("write denied")
	store := &probeStore{values: map[string]string{}, setErr: want}

	err := credentials.CheckWritable(store)
	require.ErrorIs(t, err, want)
	assert.Equal(t, 1, store.setCalls)
	assert.Zero(t, store.getCalls)
	assert.Zero(t, store.deleteCalls)
}

func TestCheckWritableRemovesProbeAfterReadFailure(t *testing.T) {
	want := errors.New("read failed")
	store := &probeStore{values: map[string]string{}, getErr: want}

	err := credentials.CheckWritable(store)
	require.ErrorIs(t, err, want)
	assert.Equal(t, 1, store.deleteCalls)
	assert.Empty(t, store.values)
}

func TestCheckWritableReturnsCleanupFailure(t *testing.T) {
	want := errors.New("delete failed")
	store := &probeStore{values: map[string]string{}, deleteErr: want}

	err := credentials.CheckWritable(store)
	require.ErrorIs(t, err, want)
}
