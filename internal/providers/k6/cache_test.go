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
		currentDomain  string
		wantOk         bool
		wantToken      string
		wantOrgID      int
	}{
		{
			name: "hit: all fields match stack",
			cfg: map[string]string{
				keyCachedToken: "tok", keyCachedOrgID: "42", keyCachedStackID: "999", keyCachedDomain: "https://api.k6.io/",
				keyCachedBinding: cacheBinding("tok", 42, 999, DefaultAPIDomain),
			},
			currentStackID: 999, currentDomain: DefaultAPIDomain, wantOk: true, wantToken: "tok", wantOrgID: 42,
		},
		{name: "miss: nil map", cfg: nil, currentStackID: 999, currentDomain: DefaultAPIDomain, wantOk: false},
		{
			name: "miss: legacy cache has no domain binding",
			cfg: map[string]string{
				keyCachedToken: "tok", keyCachedOrgID: "42", keyCachedStackID: "999",
			},
			currentStackID: 999, currentDomain: DefaultAPIDomain, wantOk: false,
		},
		{
			name: "miss: domain mismatch",
			cfg: map[string]string{
				keyCachedToken: "tok", keyCachedOrgID: "42", keyCachedStackID: "999", keyCachedDomain: "https://other.k6.invalid",
				keyCachedBinding: cacheBinding("tok", 42, 999, "https://other.k6.invalid"),
			},
			currentStackID: 999, currentDomain: DefaultAPIDomain, wantOk: false,
		},
		{
			name: "miss: empty token",
			cfg: map[string]string{
				keyCachedToken: "", keyCachedOrgID: "42", keyCachedStackID: "999", keyCachedDomain: DefaultAPIDomain,
			},
			currentStackID: 999, currentDomain: DefaultAPIDomain, wantOk: false,
		},
		{
			name: "miss: stack mismatch",
			cfg: map[string]string{
				keyCachedToken: "tok", keyCachedOrgID: "42", keyCachedStackID: "888", keyCachedDomain: DefaultAPIDomain,
				keyCachedBinding: cacheBinding("tok", 42, 888, DefaultAPIDomain),
			},
			currentStackID: 999, currentDomain: DefaultAPIDomain, wantOk: false,
		},
		{
			name: "miss: non-numeric org",
			cfg: map[string]string{
				keyCachedToken: "tok", keyCachedOrgID: "bad", keyCachedStackID: "999", keyCachedDomain: DefaultAPIDomain,
			},
			currentStackID: 999, currentDomain: DefaultAPIDomain, wantOk: false,
		},
		{
			name: "miss: non-numeric stack",
			cfg: map[string]string{
				keyCachedToken: "tok", keyCachedOrgID: "42", keyCachedStackID: "bad", keyCachedDomain: DefaultAPIDomain,
			},
			currentStackID: 999, currentDomain: DefaultAPIDomain, wantOk: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tok, org, ok := loadCache(tt.cfg, tt.currentStackID, tt.currentDomain)
			assert.Equal(t, tt.wantOk, ok)
			if tt.wantOk {
				assert.Equal(t, tt.wantToken, tok)
				assert.Equal(t, tt.wantOrgID, org)
			}
		})
	}
}

func TestLoadCache_RejectsPartiallyUpdatedOrInterleavedTuple(t *testing.T) {
	cfg := map[string]string{
		keyCachedToken:   "new-token",
		keyCachedOrgID:   "42",
		keyCachedStackID: "999",
		keyCachedDomain:  DefaultAPIDomain,
		keyCachedBinding: cacheBinding("old-token", 42, 999, DefaultAPIDomain),
	}

	_, _, ok := loadCache(cfg, 999, DefaultAPIDomain)
	assert.False(t, ok)
}

func TestPersistCache_SavesDomainBoundKeys(t *testing.T) {
	store := &fakeStore{}
	persistCache(context.Background(), store, "tok-xyz", 42, 999, "https://api.k6.io/")
	assert.Equal(t, "tok-xyz", store.saved[keyCachedToken])
	assert.Equal(t, "42", store.saved[keyCachedOrgID])
	assert.Equal(t, "999", store.saved[keyCachedStackID])
	assert.Equal(t, DefaultAPIDomain, store.saved[keyCachedDomain])
	assert.Equal(t, cacheBinding("tok-xyz", 42, 999, DefaultAPIDomain), store.saved[keyCachedBinding])
	assert.Equal(t, "k6", store.provider)
}

func TestPersistCache_SaveErrorIsNonFatal(t *testing.T) {
	store := &fakeStore{saveErr: errors.New("disk full")}
	// Must not panic; must not return anything (signature is void by design).
	persistCache(context.Background(), store, "tok", 42, 999, DefaultAPIDomain)
	assert.Empty(t, store.saved)
}

func TestClearCache_ClearsAllDomainBoundKeys(t *testing.T) {
	store := &fakeStore{}
	clearCache(context.Background(), store)
	assert.Len(t, store.saved, 5)
	assert.Empty(t, store.saved[keyCachedBinding])
	assert.Empty(t, store.saved[keyCachedToken])
	assert.Empty(t, store.saved[keyCachedOrgID])
	assert.Empty(t, store.saved[keyCachedStackID])
	assert.Empty(t, store.saved[keyCachedDomain])
	assert.Equal(t, "k6", store.provider)
}
