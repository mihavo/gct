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
	isBusy := false
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
			if isBusy == false {
				isBusy = true
			} else {
				continue
			}
			for i := 0; i < len(c.Config.Targets); i++ {
				select {
				case c.targets <- c.Config.Targets[i]:
				case <-ctx.Done():
					return
				}
			}
		case <-c.busy:
			//got notified by gatherer that results printed, workers no longer busy
			isBusy = false
		}
	}
}
