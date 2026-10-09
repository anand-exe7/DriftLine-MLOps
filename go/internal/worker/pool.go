// Package worker provides a bounded worker pool: a fixed number of goroutines
// draining a job channel. It caps concurrency (e.g. how many drift checks hit
// Postgres at once) without spawning one goroutine per job.
package worker

import (
	"context"
	"sync"
)

// Run processes every job with at most `workers` running concurrently and
// returns one result per job in the input order. It stops handing out new
// jobs once ctx is cancelled; jobs never started get ctx.Err().
func Run[J, R any](ctx context.Context, workers int, jobs []J, fn func(context.Context, J) (R, error)) ([]R, []error) {
	if workers < 1 {
		workers = 1
	}
	results := make([]R, len(jobs))
	errs := make([]error, len(jobs))

	type item struct {
		idx int
		job J
	}
	queue := make(chan item)

	var wg sync.WaitGroup
	for range min(workers, len(jobs)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for it := range queue {
				// Each goroutine writes only its own indices, so no lock is needed.
				results[it.idx], errs[it.idx] = fn(ctx, it.job)
			}
		}()
	}

feed:
	for i, j := range jobs {
		select {
		case queue <- item{i, j}:
		case <-ctx.Done():
			for k := i; k < len(jobs); k++ {
				errs[k] = ctx.Err()
			}
			break feed
		}
	}
	close(queue)
	wg.Wait()
	return results, errs
}
