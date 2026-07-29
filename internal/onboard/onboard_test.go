package onboard_test

import (
	"context"
	"errors"
	"testing"

	"github.com/grafana/gcx/internal/onboard"
)

func TestEnsureUniqueName(t *testing.T) {
	tests := []struct {
		name string
		base string
		used map[string]bool
		want string
	}{
		{name: "free", base: "gcx-azure-monitor", used: nil, want: "gcx-azure-monitor"},
		{name: "first taken", base: "gcx-azure-monitor", used: map[string]bool{"gcx-azure-monitor": true}, want: "gcx-azure-monitor-2"},
		{
			name: "first two taken",
			base: "gcx-adx",
			used: map[string]bool{"gcx-adx": true, "gcx-adx-2": true},
			want: "gcx-adx-3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := onboard.EnsureUniqueName(tt.base, func(n string) (bool, error) {
				return tt.used[n], nil
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEnsureUniqueName_PropagatesError(t *testing.T) {
	want := errors.New("boom")
	_, err := onboard.EnsureUniqueName("x", func(string) (bool, error) { return false, want })
	if !errors.Is(err, want) {
		t.Fatalf("got %v, want %v", err, want)
	}
}

func TestRollback_RunsInReverseOrder(t *testing.T) {
	var order []string
	rb := &onboard.Rollback{}
	rb.Add("first", func(context.Context) error { order = append(order, "first"); return nil })
	rb.Add("second", func(context.Context) error { order = append(order, "second"); return nil })
	rb.Add("third", func(context.Context) error { order = append(order, "third"); return nil })

	rb.Run(context.Background(), nil, nil)

	want := []string{"third", "second", "first"}
	if len(order) != len(want) {
		t.Fatalf("got %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("got %v, want %v", order, want)
		}
	}
}

func TestRollback_DescriptionsAreInRevertOrder(t *testing.T) {
	rb := &onboard.Rollback{}
	rb.Add("first", func(context.Context) error { return nil })
	rb.Add("second", func(context.Context) error { return nil })
	rb.Add("third", func(context.Context) error { return nil })

	got := rb.Descriptions()
	want := []string{"third", "second", "first"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestRollback_ContinuesAfterFailingStep(t *testing.T) {
	var ran int
	rb := &onboard.Rollback{}
	rb.Add("ok-1", func(context.Context) error { ran++; return nil })
	rb.Add("fail", func(context.Context) error { return errors.New("nope") })
	rb.Add("ok-2", func(context.Context) error { ran++; return nil })

	rb.Run(context.Background(), nil, nil)

	if ran != 2 {
		t.Fatalf("expected both non-failing steps to run, got %d", ran)
	}
}
