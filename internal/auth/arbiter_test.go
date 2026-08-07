package auth_test

import (
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The arbiter is the one place that decides who may run a token exchange, so
// its contract is tested directly rather than only through a live flow. These
// cases are deterministic: no listener, no browser, no timing luck.

const testDeadline = time.Hour // long enough never to fire during a test

func TestCallbackArbiter_FirstClaimWinsAndReplayIsRejected(t *testing.T) {
	arb := auth.NewCallbackArbiter(testDeadline)
	t.Cleanup(arb.Stop)

	require.Equal(t, auth.ClaimGranted, arb.Claim(), "the first claim owns the flow")
	assert.Equal(t, auth.ClaimTaken, arb.Claim(), "a second claim must not start a parallel exchange")

	arb.Settle()
	assert.Equal(t, auth.ClaimTaken, arb.Claim(), "a replay after success stays rejected")
}

func TestCallbackArbiter_ConcurrentClaimsGrantExactlyOne(t *testing.T) {
	const claimants = 32

	arb := auth.NewCallbackArbiter(testDeadline)
	t.Cleanup(arb.Stop)

	var (
		start   = make(chan struct{})
		wg      sync.WaitGroup
		mu      sync.Mutex
		granted int
	)
	for range claimants {
		wg.Go(func() {
			<-start
			if arb.Claim() == auth.ClaimGranted {
				mu.Lock()
				granted++
				mu.Unlock()
			}
		})
	}
	close(start)
	wg.Wait()

	assert.Equal(t, 1, granted, "exactly one claimant may run the exchange")
}

func TestCallbackArbiter_ExpiresWhenNoCallbackArrives(t *testing.T) {
	arb := auth.NewCallbackArbiter(time.Millisecond)
	t.Cleanup(arb.Stop)

	select {
	case <-arb.Expired():
	case <-time.After(5 * time.Second):
		t.Fatal("the flow must not wait forever for a callback that never comes")
	}

	assert.Equal(t, auth.ClaimExpired, arb.Claim(), "a callback after the deadline is told the flow ended")
}

// A callback arriving exactly at the deadline must produce one coherent
// outcome. It may lose the race, but it may never half-claim a flow that is
// expiring, and it may never win a flow that already reported a timeout.
func TestCallbackArbiter_ClaimRacingDeadlineHasOneOutcome(t *testing.T) {
	for range 200 {
		arb := auth.NewCallbackArbiter(testDeadline)

		var (
			start   = make(chan struct{})
			wg      sync.WaitGroup
			verdict auth.CallbackClaim
		)
		wg.Go(func() { <-start; arb.DeadlineReached() })
		wg.Go(func() { <-start; verdict = arb.Claim() })
		close(start)
		wg.Wait()

		expired := false
		select {
		case <-arb.Expired():
			expired = true
		default:
		}

		switch verdict {
		case auth.ClaimGranted:
			require.False(t, expired, "a granted claim must stop the flow from also timing out")
		case auth.ClaimExpired:
			require.True(t, expired, "a claim told the flow expired must see it actually expire")
		case auth.ClaimTaken:
			t.Fatal("nothing else holds this flow")
		}
		arb.Stop()
	}
}

func TestCallbackArbiter_DeadlineAfterSuccessIsIgnored(t *testing.T) {
	arb := auth.NewCallbackArbiter(testDeadline)
	t.Cleanup(arb.Stop)

	require.Equal(t, auth.ClaimGranted, arb.Claim())
	arb.Settle()

	arb.DeadlineReached()
	select {
	case <-arb.Expired():
		t.Fatal("a finished flow must not report a timeout")
	default:
	}
}

func TestCallbackBelongsToFlow(t *testing.T) {
	tests := []struct {
		name     string
		query    url.Values
		expected string
		want     bool
	}{
		{"matching state", url.Values{"state": {"abc"}}, "abc", true},
		{"different state", url.Values{"state": {"xyz"}}, "abc", false},
		{"missing state", url.Values{"code": {"c"}}, "abc", false},
		{"empty state", url.Values{"state": {""}}, "abc", false},
		{"prefix of the real state", url.Values{"state": {"ab"}}, "abc", false},
		{"state with the real one as a prefix", url.Values{"state": {"abcd"}}, "abc", false},
		{"no expected state", url.Values{"state": {"abc"}}, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, auth.CallbackBelongsToFlow(tt.query, tt.expected))
		})
	}
}
