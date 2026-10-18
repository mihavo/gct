package gate

import (
	"context"
	"log/slog"
	"net/http"
	"time"
	"uuid"
)

type Middleware interface {
	Process(next http.Handler) http.Handler
}

type LoggingMiddleware struct {
}

func (l *LoggingMiddleware) Process(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		rwWrapper := ResponseWriterWithCode{
			code:           200,
			ResponseWriter: w,
		}
		requestId := uuid.New()
		start := time.Now()
		r = r.WithContext(context.WithValue(r.Context(), RequestID{}, requestId))
		defer func() {
			slog.Info("Request Details: ", "RequestID", requestId, "method", r.Method, "path", r.URL.Path, "status", rwWrapper.code, "duration:", time.Since(start))
		}()
		next.ServeHTTP(&rwWrapper, r)
	})
}
