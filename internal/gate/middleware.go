package gate

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
	"uuid"

	appjson "github.com/mihavo/gct/internal/core/json"
)

type Middleware interface {
	Process(next http.Handler) http.Handler
}

type LoggingMiddleware struct {
}

type RecoveryMiddleware struct {
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

func (r *RecoveryMiddleware) Process(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			err := recover()
			if err != nil {
				if err, ok := err.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					panic(err)
				}
				log := slog.Default()
				message := "unexpected error"
				if requestID, ok := r.Context().Value(RequestID{}).(uuid.UUID); ok {
					log = log.With("RequestID", requestID)
					message = "error in request: " + requestID.String()
				}
				log.Error("panic while serving request", "error", err)
				appjson.WriteError(w, message, http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func Chain(h http.Handler, middlewares ...Middleware) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i].Process(h)
	}
	return h
}
