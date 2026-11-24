package serve

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

func Run(ctx context.Context, server *http.Server, shutdownTimeout time.Duration) error {
	errCh := make(chan error, 1)

	go func() {
		errCh <- server.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("serving on %s: %w", server.Addr, err)
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutting down %s: %w", server.Addr, err)
	}

	return nil
}
