package framework

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"golang.org/x/sync/errgroup"
)

const defaultAggregateTimeout = 5 * time.Second

// AggregateStatus calls Status() on every StatusDetectable provider in parallel.
// timeout <= 0 defaults to 5 seconds.
// Returns []ProductStatus sorted alphabetically by ProductName().
func AggregateStatus(ctx context.Context, timeout time.Duration) []ProductStatus {
	return AggregateStatusFrom(ctx, timeout, DiscoverStatusDetectable())
}

// AggregateStatusFrom is like AggregateStatus but operates on an explicit
// provider list instead of the global registry. Useful for testing.
func AggregateStatusFrom(ctx context.Context, timeout time.Duration, providers []StatusDetectable) []ProductStatus {
	if timeout <= 0 {
		timeout = defaultAggregateTimeout
	}

	results := make([]ProductStatus, len(providers))

	g := new(errgroup.Group)
	g.SetLimit(10)

	for i, sd := range providers {
		i, sd := i, sd
		g.Go(func() error {
			cctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			results[i] = collectStatus(cctx, sd, timeout)
			return nil
		})
	}

	_ = g.Wait()

	slices.SortFunc(results, func(a, b ProductStatus) int {
		return cmp.Compare(a.Product, b.Product)
	})

	return results
}

// collectStatus calls sd.Status in an isolated goroutine, recovering panics and
// enforcing the per-provider deadline via a select on cctx.Done().
// It always sets result.Product = sd.ProductName().
func collectStatus(cctx context.Context, sd StatusDetectable, timeout time.Duration) ProductStatus {
	name := sd.ProductName()
	done := make(chan ProductStatus, 1)

	go func() {
		var ps ProductStatus
		defer func() {
			if r := recover(); r != nil {
				ps = ProductStatus{
					Product: name,
					State:   StateError,
					Details: fmt.Sprintf("panic: %v", r),
				}
			}
			done <- ps
		}()

		status, err := sd.Status(cctx)
		if err != nil {
			ps = ProductStatus{Product: name, State: StateError, Details: err.Error()}
			return
		}
		if status == nil {
			ps = ProductStatus{Product: name, State: StateError, Details: "nil status returned"}
			return
		}
		ps = *status
		ps.Product = name
	}()

	select {
	case s := <-done:
		return s
	case <-cctx.Done():
		return ProductStatus{
			Product: name,
			State:   StateError,
			Details: fmt.Sprintf("status check timed out after %v", timeout),
		}
	}
}
