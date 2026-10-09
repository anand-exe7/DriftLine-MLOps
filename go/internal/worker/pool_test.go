package worker

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunPreservesOrderAndBoundsConcurrency(t *testing.T) {
	var running, peak atomic.Int32
	jobs := make([]int, 40)
	for i := range jobs {
		jobs[i] = i
	}
	results, errs := Run(context.Background(), 3, jobs, func(_ context.Context, j int) (int, error) {
		n := running.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(2 * time.Millisecond)
		running.Add(-1)
		if j == 7 {
			return 0, errors.New("boom")
		}
		return j * j, nil
	})
	if peak.Load() > 3 {
		t.Fatalf("peak concurrency %d exceeds 3 workers", peak.Load())
	}
	for i := range jobs {
		if i == 7 {
			if errs[i] == nil {
				t.Fatal("job 7 should carry its error")
			}
			continue
		}
		if errs[i] != nil || results[i] != i*i {
			t.Fatalf("job %d: result %d err %v", i, results[i], errs[i])
		}
	}
}

func TestRunStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, errs := Run(ctx, 2, []int{1, 2, 3}, func(context.Context, int) (int, error) { return 0, nil })
	cancelled := 0
	for _, err := range errs {
		if errors.Is(err, context.Canceled) {
			cancelled++
		}
	}
	if cancelled == 0 {
		t.Fatal("expected jobs after cancellation to report context.Canceled")
	}
}
