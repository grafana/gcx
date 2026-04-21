package framework

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"golang.org/x/sync/errgroup"
)

// AggregateStatus calls Status() on every StatusDetectable provider in parallel.
// Returns []ProductStatus sorted alphabetically by ProductName().
// Deadline management is the caller's responsibility via ctx.
func AggregateStatus(ctx context.Context) []ProductStatus {
	return AggregateStatusFrom(ctx, DiscoverStatusDetectable())
}

// AggregateStatusFrom is like AggregateStatus but operates on an explicit
// provider list instead of the global registry. Useful for testing.
func AggregateStatusFrom(ctx context.Context, providers []StatusDetectable) []ProductStatus {
	results := make([]ProductStatus, len(providers))

	g := new(errgroup.Group)
	g.SetLimit(10)

	for i, sd := range providers {
		g.Go(func() error {
			results[i] = collectStatus(ctx, sd)
			return nil
		})
	}

	_ = g.Wait() // goroutines always return nil; errors are collected in collectStatus

	slices.SortFunc(results, func(a, b ProductStatus) int {
		return cmp.Compare(a.Product, b.Product)
	})

	return results
}

// collectStatus calls sd.Status in an isolated goroutine, recovering panics and
// propagating context cancellation via a select on ctx.Done().
// It always sets result.Product = sd.ProductName().
// The buffered done channel (size 1) prevents the goroutine from blocking if ctx
// is cancelled before the goroutine writes its result.
func collectStatus(ctx context.Context, sd StatusDetectable) ProductStatus {
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

		status, err := sd.Status(ctx)
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
	case <-ctx.Done():
		return ProductStatus{
			Product: name,
			State:   StateError,
			Details: "status check cancelled",
		}
	}
}
