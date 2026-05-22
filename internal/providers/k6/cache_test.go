package k6 //nolint:testpackage // tests exercise unexported cache helpers.

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

type fakeStore struct {
	saved    map[string]string
	provider string
	saveErr  error
}

func (f *fakeStore) SaveProviderConfig(_ context.Context, providerName, key, value string) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	if f.saved == nil {
		f.saved = make(map[string]string)
	}
	f.provider = providerName
	f.saved[key] = value
	return nil
}

func TestLoadCache(t *testing.T) {
	tests := []struct {
		name           string
		cfg            map[string]string
		currentStackID int
		wantOk         bool
		wantToken      string
		wantOrgID      int
	}{
		{
			name: "hit: all fields match stack",
			cfg: map[string]string{
				keyCachedToken: "tok", keyCachedOrgID: "42", keyCachedStackID: "999",
			},
			currentStackID: 999, wantOk: true, wantToken: "tok", wantOrgID: 42,
		},
		{name: "miss: nil map", cfg: nil, currentStackID: 999, wantOk: false},
		{
			name: "miss: empty token",
			cfg: map[string]string{
				keyCachedToken: "", keyCachedOrgID: "42", keyCachedStackID: "999",
			},
			currentStackID: 999, wantOk: false,
		},
		{
			name: "miss: stack mismatch",
			cfg: map[string]string{
				keyCachedToken: "tok", keyCachedOrgID: "42", keyCachedStackID: "888",
			},
			currentStackID: 999, wantOk: false,
		},
		{
			name: "miss: non-numeric org",
			cfg: map[string]string{
				keyCachedToken: "tok", keyCachedOrgID: "bad", keyCachedStackID: "999",
			},
			currentStackID: 999, wantOk: false,
		},
		{
			name: "miss: non-numeric stack",
			cfg: map[string]string{
				keyCachedToken: "tok", keyCachedOrgID: "42", keyCachedStackID: "bad",
			},
			currentStackID: 999, wantOk: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tok, org, ok := loadCache(tt.cfg, tt.currentStackID)
			assert.Equal(t, tt.wantOk, ok)
			if tt.wantOk {
				assert.Equal(t, tt.wantToken, tok)
				assert.Equal(t, tt.wantOrgID, org)
			}
		})
	}
}

func TestPersistCache_SavesAllThreeKeys(t *testing.T) {
	store := &fakeStore{}
	persistCache(context.Background(), store, "tok-xyz", 42, 999)
	assert.Equal(t, "tok-xyz", store.saved[keyCachedToken])
	assert.Equal(t, "42", store.saved[keyCachedOrgID])
	assert.Equal(t, "999", store.saved[keyCachedStackID])
	assert.Equal(t, "k6", store.provider)
}

func TestPersistCache_SaveErrorIsNonFatal(t *testing.T) {
	store := &fakeStore{saveErr: errors.New("disk full")}
	// Must not panic; must not return anything (signature is void by design).
	persistCache(context.Background(), store, "tok", 42, 999)
	assert.Empty(t, store.saved)
}

func TestClearCache_ClearsAllThreeKeys(t *testing.T) {
	store := &fakeStore{}
	clearCache(context.Background(), store)
	assert.Len(t, store.saved, 3)
	assert.Empty(t, store.saved[keyCachedToken])
	assert.Empty(t, store.saved[keyCachedOrgID])
	assert.Empty(t, store.saved[keyCachedStackID])
	assert.Equal(t, "k6", store.provider)
}
