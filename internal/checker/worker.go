package checker

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"
)

func (c *Checker) Check(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			slog.Info("Shutting down worker...")
			return
		case target, ok := <-c.targets:
			if !ok {
				return
			}
			start := time.Now()

			reqCtx, cancel := context.WithTimeout(ctx, c.Config.Timeout)
			request, err := http.NewRequestWithContext(reqCtx, http.MethodGet, target.URL, nil)

			if err != nil {
				cancel()
				select {
				case c.results <- Result{
					duration:       time.Since(start),
					targetName:     target.Name,
					expectedStatus: target.ExpectedStatus,
					errorData:      err,
					outcome:        OutcomeInvalid,
				}:
					continue
				case <-ctx.Done():
					return
				}
			}

			response, err := c.client.Do(request)
			cancel()
			elapsed := time.Since(start)

			result := Result{
				duration:       elapsed,
				targetName:     target.Name,
				expectedStatus: target.ExpectedStatus,
				outcome:        OutcomePending,
			}

			if err != nil {
				var netErr net.Error
				var opErr *net.OpError
				switch {
				case errors.Is(err, context.DeadlineExceeded),
					errors.As(err, &netErr) && netErr.Timeout():
					result.outcome = OutcomeTimeout
				case errors.As(err, &opErr) && opErr.Op == "dial":
					result.outcome = OutcomeUnreachable
				default:
					result.outcome = OutcomeUnknownError
				}
				result.errorData = err
			} else {
				if response.StatusCode != target.ExpectedStatus {
					result.outcome = OutcomeFail
				} else {
					result.outcome = OutcomeOK
				}
				result.statusCode = response.StatusCode
				response.Body.Close()
			}

			select {
			case c.results <- result:
			case <-ctx.Done():
				slog.Info("Shutting down worker, dropping result...")
				return
			}
		}
	}
}
