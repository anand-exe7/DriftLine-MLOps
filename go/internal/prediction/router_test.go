package prediction

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func routes(weights ...int) []Route {
	out := make([]Route, len(weights))
	for i, w := range weights {
		out[i] = Route{Version: fmt.Sprintf("v%d", i+1), Weight: w}
	}
	return out
}

func TestPickSplitsTrafficExactly(t *testing.T) {
	tests := []struct {
		name    string
		weights []int
	}{
		{"90/10", []int{90, 10}},
		{"50/50", []int{50, 50}},
		{"99/1", []int{99, 1}},
		{"single route", []int{100}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRouter(nil, time.Minute)
			rs := routes(tc.weights...)
			counts := map[string]int{}
			for range 1000 {
				rt, ok := r.Pick("m", "", rs)
				if !ok {
					t.Fatal("no route")
				}
				counts[rt.Version]++
			}
			for i, w := range tc.weights {
				if got := counts[fmt.Sprintf("v%d", i+1)]; got != w*10 {
					t.Errorf("v%d got %d of 1000, want %d", i+1, got, w*10)
				}
			}
		})
	}
}

func TestPickIsConcurrencySafe(t *testing.T) {
	r := NewRouter(nil, time.Minute)
	rs := routes(80, 20)
	var canary atomic.Int64
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				if rt, _ := r.Pick("m", "", rs); rt.Version == "v2" {
					canary.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	// 10,000 picks from one shared counter: exactly 20% go to the canary.
	if got := canary.Load(); got != 2000 {
		t.Fatalf("canary got %d of 10000, want 2000", got)
	}
}

func TestPickWithRoutingKeyIsSticky(t *testing.T) {
	r := NewRouter(nil, time.Minute)
	rs := routes(50, 50)
	first, _ := r.Pick("m", "customer-42", rs)
	for range 100 {
		if rt, _ := r.Pick("m", "customer-42", rs); rt.Version != first.Version {
			t.Fatal("same routing key must always pick the same version")
		}
	}
}

func TestPickNoRoutes(t *testing.T) {
	if _, ok := NewRouter(nil, time.Minute).Pick("m", "", nil); ok {
		t.Fatal("expected no route")
	}
}

type countingSource struct {
	calls atomic.Int32
}

func (c *countingSource) Routes(context.Context, string) ([]Route, error) {
	c.calls.Add(1)
	return routes(100), nil
}

func TestRoutesAreCachedAndInvalidated(t *testing.T) {
	src := &countingSource{}
	r := NewRouter(src, time.Hour)
	ctx := context.Background()

	for range 5 {
		if _, err := r.Routes(ctx, "m"); err != nil {
			t.Fatal(err)
		}
	}
	if src.calls.Load() != 1 {
		t.Fatalf("source called %d times, want 1 (cached)", src.calls.Load())
	}
	r.Invalidate("m")
	_, _ = r.Routes(ctx, "m")
	if src.calls.Load() != 2 {
		t.Fatalf("source called %d times after invalidate, want 2", src.calls.Load())
	}
}
