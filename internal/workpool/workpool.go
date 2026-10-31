// Package workpool runs a function over items with a fixed number of goroutines.
package workpool

import (
	"context"
	"sync"
)

// ForEach calls fn for every item using at most workers goroutines and
// returns ctx.Err() if cancelled before all items were handed out.
func ForEach[T any](ctx context.Context, workers int, items []T, fn func(T)) error {
	ch := make(chan T)
	var wg sync.WaitGroup
	for range min(max(workers, 1), max(len(items), 1)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for it := range ch {
				if ctx.Err() == nil {
					fn(it)
				}
			}
		}()
	}
loop:
	for _, it := range items {
		select {
		case ch <- it:
		case <-ctx.Done():
			break loop
		}
	}
	close(ch)
	wg.Wait()
	return ctx.Err()
}
