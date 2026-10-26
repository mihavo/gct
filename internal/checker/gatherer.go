package checker

import (
	"context"
	"log/slog"
	"os"
	"time"
)

func (c *Checker) GatherAndPresent(ctx context.Context) {

	results := make(map[string]Result, len(c.Config.Targets))
	table := newTableWriter(os.Stdout)

	ticker := time.NewTicker(c.Config.Interval)
	defer ticker.Stop()
	defer close(c.busy)

	for {
		select {
		case <-ctx.Done():
			slog.Info("Shutting down gatherer:")
			return
		case result, ok := <-c.results:
			if !ok {
				slog.Info("Shutting down gatherer, results channel closed")
				return
			}
			results[result.targetName] = result
			select {
			case c.busy <- result.targetName:
			case <-ctx.Done():
				slog.Info("Shutting down gatherer:")
				return
			}
		case <-ticker.C:
			rows := make([]Result, 0, len(c.Config.Targets))
			for _, target := range c.Config.Targets {
				row, ok := results[target.Name]
				if !ok {
					row = Result{
						targetName:     target.Name,
						expectedStatus: target.ExpectedStatus,
						outcome:        OutcomePending,
					}
				}
				rows = append(rows, row)
			}

			if err := table.render(rows); err != nil {
				slog.Error("rendering results", "error", err)
			}
		}
	}

}
