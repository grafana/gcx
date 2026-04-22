package framework_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/setup/framework"
	"github.com/grafana/gcx/internal/setup/framework/testhelpers"
)

// Deadline/timeout enforcement is the caller's responsibility via context.
// Pass a context.WithTimeout or context.WithDeadline to AggregateStatus /
// AggregateStatusFrom when bounded execution time is required.

func TestAggregateStatusFrom(t *testing.T) {
	active := func(name string) *framework.ProductStatus {
		return &framework.ProductStatus{Product: name, State: framework.StateActive}
	}
	configured := func(name string) *framework.ProductStatus {
		return &framework.ProductStatus{Product: name, State: framework.StateConfigured}
	}
	notConfigured := func(name string) *framework.ProductStatus {
		return &framework.ProductStatus{Product: name, State: framework.StateNotConfigured}
	}

	t.Run("happy path: mixed states returned in alphabetical order", func(t *testing.T) {
		providers := []framework.StatusDetectable{
			&testhelpers.FakeStatusDetectable{ProductName_: "C-provider", Status_: active("C-provider")},
			&testhelpers.FakeStatusDetectable{ProductName_: "A-provider", Status_: configured("A-provider")},
			&testhelpers.FakeStatusDetectable{ProductName_: "B-provider", Status_: notConfigured("B-provider")},
		}

		got := aggregateFrom(t, providers)

		requireLen(t, got, 3)
		requireProduct(t, got[0], "A-provider", framework.StateConfigured)
		requireProduct(t, got[1], "B-provider", framework.StateNotConfigured)
		requireProduct(t, got[2], "C-provider", framework.StateActive)
	})

	t.Run("error isolation: one error does not affect siblings", func(t *testing.T) {
		errMsg := "connection refused"
		providers := []framework.StatusDetectable{
			&testhelpers.FakeStatusDetectable{ProductName_: "A-ok", Status_: active("A-ok")},
			&testhelpers.FakeStatusDetectable{ProductName_: "B-err", Err: errorf(errMsg)},
			&testhelpers.FakeStatusDetectable{ProductName_: "C-ok", Status_: active("C-ok")},
		}

		got := aggregateFrom(t, providers)

		requireLen(t, got, 3)
		requireProduct(t, got[0], "A-ok", framework.StateActive)
		requireProduct(t, got[1], "B-err", framework.StateError)
		if got[1].Details != errMsg {
			t.Errorf("error details: want %q, got %q", errMsg, got[1].Details)
		}
		requireProduct(t, got[2], "C-ok", framework.StateActive)
	})

	// Timeout/deadline enforcement is the caller's responsibility via context.
	// Pass context.WithTimeout to AggregateStatusFrom when bounded execution is needed.

	t.Run("panic: panicking provider is StateError; siblings succeed", func(t *testing.T) {
		providers := []framework.StatusDetectable{
			&testhelpers.FakeStatusDetectable{ProductName_: "A-ok", Status_: active("A-ok")},
			&testhelpers.FakeStatusDetectable{ProductName_: "B-panic", ShouldPanic: true},
			&testhelpers.FakeStatusDetectable{ProductName_: "C-ok", Status_: active("C-ok")},
		}

		got := aggregateFrom(t, providers)

		requireLen(t, got, 3)
		requireProduct(t, got[0], "A-ok", framework.StateActive)
		requireProduct(t, got[1], "B-panic", framework.StateError)
		requireProduct(t, got[2], "C-ok", framework.StateActive)
	})

	t.Run("ordering: completion order C,A,B → result order A,B,C", func(t *testing.T) {
		// C completes first (no latency), A second (small latency), B last (larger latency)
		// but results should still be alphabetical.
		providers := []framework.StatusDetectable{
			&testhelpers.FakeStatusDetectable{ProductName_: "C-third-alpha", Status_: active("C-third-alpha")},
			&testhelpers.FakeStatusDetectable{ProductName_: "A-first-alpha", Latency: 10 * time.Millisecond, Status_: active("A-first-alpha")},
			&testhelpers.FakeStatusDetectable{ProductName_: "B-second-alpha", Latency: 20 * time.Millisecond, Status_: active("B-second-alpha")},
		}

		got := aggregateFrom(t, providers)

		requireLen(t, got, 3)
		requireProduct(t, got[0], "A-first-alpha", framework.StateActive)
		requireProduct(t, got[1], "B-second-alpha", framework.StateActive)
		requireProduct(t, got[2], "C-third-alpha", framework.StateActive)
	})

	t.Run("bounded parallelism: max concurrency <= 10", func(t *testing.T) {
		var current atomic.Int64
		var maxSeen atomic.Int64

		makeProvider := func(name string) framework.StatusDetectable {
			return &concurrencyProbeProvider{
				name:    name,
				current: &current,
				maxSeen: &maxSeen,
			}
		}

		providers := make([]framework.StatusDetectable, 20)
		for i := range providers {
			providers[i] = makeProvider(providerName(i))
		}

		got := aggregateFrom(t, providers)
		requireLen(t, got, 20)

		if maxSeen.Load() > 10 {
			t.Errorf("max concurrency %d exceeds limit of 10", maxSeen.Load())
		}
	})
}

// helpers

func aggregateFrom(t *testing.T, providers []framework.StatusDetectable) []framework.ProductStatus {
	t.Helper()
	return framework.AggregateStatusFrom(context.Background(), providers)
}

func requireLen(t *testing.T, got []framework.ProductStatus, want int) {
	t.Helper()
	if len(got) != want {
		t.Fatalf("len(results) = %d, want %d", len(got), want)
	}
}

func requireProduct(t *testing.T, ps framework.ProductStatus, wantProduct string, wantState framework.ProductState) {
	t.Helper()
	if ps.Product != wantProduct {
		t.Errorf("Product = %q, want %q", ps.Product, wantProduct)
	}
	if ps.State != wantState {
		t.Errorf("[%s] State = %q, want %q", ps.Product, ps.State, wantState)
	}
}

func errorf(msg string) error {
	return &simpleError{msg}
}

type simpleError struct{ msg string }

func (e *simpleError) Error() string { return e.msg }

func providerName(i int) string {
	return fmt.Sprintf("provider-%02d", i)
}

// concurrencyProbeProvider tracks concurrent invocations.
type concurrencyProbeProvider struct {
	name    string
	current *atomic.Int64
	maxSeen *atomic.Int64
}

func (p *concurrencyProbeProvider) ProductName() string { return p.name }

func (p *concurrencyProbeProvider) Status(_ context.Context) (*framework.ProductStatus, error) {
	cur := p.current.Add(1)
	defer p.current.Add(-1)

	// update maxSeen
	for {
		old := p.maxSeen.Load()
		if cur <= old {
			break
		}
		if p.maxSeen.CompareAndSwap(old, cur) {
			break
		}
	}

	// small sleep to allow concurrent goroutines to overlap
	time.Sleep(5 * time.Millisecond)

	return &framework.ProductStatus{Product: p.name, State: framework.StateActive}, nil
}
