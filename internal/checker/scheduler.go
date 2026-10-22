package checker

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

func (c *Checker) Schedule(ctx context.Context) {
	ticker := time.NewTicker(c.Config.Interval)
	var wg sync.WaitGroup
	wg.Go(func() { c.GatherAndPresent(ctx) })
	for i := 0; i < c.Config.Workers; i++ {
		wg.Go(func() {
			c.Check(ctx)
		})
	}

	for {
		select {
		case <-ctx.Done():
			ticker.Stop()
			wg.Wait()
			slog.Info("Shutting down scheduler...")
			return
		case <-ticker.C:
			for i := 0; i < len(c.Config.Targets); i++ {
				c.targets <- c.Config.Targets[i]
			}
		}
	}
}
