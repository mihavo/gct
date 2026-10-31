package gate

import (
	"context"
	"errors"
	"log/slog"
	"net"
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

type LimiterMiddleware struct {
	limiter *Limiter
}

func (l *LimiterMiddleware) Process(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		message := "unexpected error"
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			slog.Error("error parsing request IP", "error", err, "RemoteAddr", r.RemoteAddr)
			appjson.WriteError(w, message, http.StatusInternalServerError)
			return
		}
		err = l.limiter.limit(r.Context(), ip, time.Now())
		if err != nil {
			if errors.Is(err, ErrRetriesExceeded) || errors.Is(err, ErrRateLimited) {
				slog.Debug("request denied by rate limiter", "error", err, "ip", ip, "method", r.Method, "path", r.URL.Path)
				appjson.WriteError(w, err.Error(), http.StatusTooManyRequests)
				return
			}
			slog.Error("rate limiter failed", "error", err, "ip", ip, "method", r.Method, "path", r.URL.Path)
			appjson.WriteError(w, message, http.StatusInternalServerError)
			return
		}
		next.ServeHTTP(w, r)
	})
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
