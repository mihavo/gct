package checker

import (
	"context"
	"log/slog"
	"os"
)

func (c *Checker) GatherAndPresent(ctx context.Context) {

	results := make([]Result, 0, len(c.Config.Targets))
	table := newTableWriter(os.Stdout)
	for {

		select {
		case <-ctx.Done():
			slog.Info("Shutting down gatherer:")
			return
		case result, ok := <-c.results:
			if !ok {
				continue
			}
			results = append(results, result)
			if len(results) == len(c.Config.Targets) {
				if err := table.render(results); err != nil {
					slog.Error("rendering results", "error", err)
				}
				results = results[:0]
			}
		}
	}

}
