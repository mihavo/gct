package checker

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

func (c *Checker) Schedule(ctx context.Context) {
	defer slog.Info("Shutting down scheduler...")
	ticker := time.NewTicker(c.Config.Interval)
	defer ticker.Stop()
	busy := map[string]struct{}{}
	var wg sync.WaitGroup
	wg.Go(func() { c.GatherAndPresent(ctx) })
	for i := 0; i < c.Config.Workers; i++ {
		wg.Go(func() {
			c.Check(ctx)
		})
	}
	defer wg.Wait()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for i := 0; i < len(c.Config.Targets); i++ {
				if _, ok := busy[c.Config.Targets[i].Name]; ok {
					continue
				}
				select {
				case c.targets <- c.Config.Targets[i]:
					busy[c.Config.Targets[i].Name] = struct{}{}
				case target := <-c.busy:
					//got notified by gatherer that a target is unblockd, worker no longer busy
					delete(busy, target)
					i--
					continue
				case <-ctx.Done():
					return
				}
			}
		case target := <-c.busy:
			delete(busy, target)
		}
	}
}
