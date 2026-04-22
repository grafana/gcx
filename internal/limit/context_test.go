package limit_test

import (
	"context"
	"testing"

	"github.com/grafana/gcx/internal/limit"
)

func TestResolve(t *testing.T) {
	tests := []struct {
		name           string
		setupCtx       func() context.Context
		commandDefault int64
		want           int64
	}{
		{
			name:           "no context value uses command default",
			setupCtx:       context.Background,
			commandDefault: 50,
			want:           50,
		},
		{
			name:           "explicit flag overrides command default",
			setupCtx:       func() context.Context { return limit.WithLimit(context.Background(), 10, true) },
			commandDefault: 50,
			want:           10,
		},
		{
			name:           "explicit zero means unlimited",
			setupCtx:       func() context.Context { return limit.WithLimit(context.Background(), 0, true) },
			commandDefault: 50,
			want:           0,
		},
		{
			name:           "agent default overrides unlimited command",
			setupCtx:       func() context.Context { return limit.WithLimit(context.Background(), 50, false) },
			commandDefault: 0,
			want:           50,
		},
		{
			name:           "agent default does not override non-zero command default",
			setupCtx:       func() context.Context { return limit.WithLimit(context.Background(), 50, false) },
			commandDefault: 100,
			want:           100,
		},
		{
			name:           "agent default does not override smaller command default",
			setupCtx:       func() context.Context { return limit.WithLimit(context.Background(), 50, false) },
			commandDefault: 20,
			want:           20,
		},
		{
			name:           "explicit flag overrides even when command default is zero",
			setupCtx:       func() context.Context { return limit.WithLimit(context.Background(), 5, true) },
			commandDefault: 0,
			want:           5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := limit.Resolve(tt.setupCtx(), tt.commandDefault)
			if got != tt.want {
				t.Errorf("Resolve() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestFromContext(t *testing.T) {
	t.Run("missing returns false", func(t *testing.T) {
		_, ok := limit.FromContext(context.Background())
		if ok {
			t.Error("expected ok=false for empty context")
		}
	})

	t.Run("round-trip explicit", func(t *testing.T) {
		ctx := limit.WithLimit(context.Background(), 42, true)
		v, ok := limit.FromContext(ctx)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if v.N != 42 || !v.Explicit {
			t.Errorf("got {N: %d, Explicit: %v}, want {N: 42, Explicit: true}", v.N, v.Explicit)
		}
	})

	t.Run("round-trip implicit", func(t *testing.T) {
		ctx := limit.WithLimit(context.Background(), 50, false)
		v, ok := limit.FromContext(ctx)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if v.N != 50 || v.Explicit {
			t.Errorf("got {N: %d, Explicit: %v}, want {N: 50, Explicit: false}", v.N, v.Explicit)
		}
	})
}
