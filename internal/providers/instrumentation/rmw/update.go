package rmw

import (
	"context"
	"errors"
)

// Update performs a read-modify-write with a client-side optimistic-lock check.
//
// On each attempt:
//  1. getFn is called to capture snapshot1.
//  2. mutateFn(snapshot1) produces the proposed write value.
//  3. getFn is called again to capture snapshot2 immediately before writing.
//  4. equalsFn(snapshot1, snapshot2) is called; if they differ, Update returns a
//     ConflictError immediately — conflicts are never retried.
//  5. setFn(ctx, proposed) is called; on success, nil is returned.
//
// If setFn returns a non-nil, non-ConflictError error, the entire sequence retries
// up to maxRetries additional times (1 + maxRetries total attempts). Each retry starts
// with a fresh getFn call — stale snapshots are never reused.
//
// The returned ConflictError has Diff populated from equalsFn; Command and Namespace
// are left empty and must be filled in by the calling command layer before surfacing
// the error to the user.
func Update[T any](
	ctx context.Context,
	getFn func(context.Context) (T, error),
	mutateFn func(T) T,
	setFn func(context.Context, T) error,
	equalsFn func(T, T) (bool, string),
	maxRetries int,
) error {
	for attempt := 0; attempt <= maxRetries; attempt++ {
		// Step 1: capture initial snapshot.
		snapshot1, err := getFn(ctx)
		if err != nil {
			return err
		}

		// Step 2: apply mutation to produce the proposed value.
		proposed := mutateFn(snapshot1)

		// Step 3: re-fetch immediately before writing.
		snapshot2, err := getFn(ctx)
		if err != nil {
			return err
		}

		// Step 4: compare snapshot1 (at-read) vs snapshot2 (pre-write).
		// A difference means a concurrent modification — fail fast, never retry.
		equal, diff := equalsFn(snapshot1, snapshot2)
		if !equal {
			return ConflictError{Diff: diff}
		}

		// Step 5: write the proposed value.
		err = setFn(ctx, proposed)
		if err == nil {
			return nil
		}

		// ConflictError from setFn is never retried.
		var ce ConflictError
		if errors.As(err, &ce) {
			return err
		}

		// On the last attempt, return the error as-is.
		if attempt == maxRetries {
			return err
		}
		// Otherwise, loop — next iteration re-fetches from scratch.
	}
	return nil
}
