package gate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"time"
	"uuid"

	appjson "github.com/mihavo/gct/internal/core/json"
	"github.com/redis/go-redis/v9"
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

type AuthMiddleware struct {
	redis *redis.Client
}

func (l *LimiterMiddleware) Process(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		message := "unexpected error"
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			slog.Error("error splitting host from port", "error", err, "RemoteAddr", r.RemoteAddr)
			appjson.WriteError(w, message, http.StatusInternalServerError)
			return
		}
		addr, err := netip.ParseAddr(ip)
		if err != nil {
			slog.Error("error parsing address", "error", err, "RemoteAddr", r.RemoteAddr)
			appjson.WriteError(w, message, http.StatusInternalServerError)
			return
		}
		callerIP := addr.Unmap().String()
		ctx, cancel := context.WithTimeout(r.Context(), time.Millisecond*100)
		defer cancel()
		err = l.limiter.limit(ctx, callerIP)
		if err != nil {
			if errors.Is(err, ErrRetriesExceeded) || errors.Is(err, ErrRateLimited) {
				slog.Debug("request denied by rate limiter", "error", err, "ip", callerIP, "method", r.Method, "path", r.URL.Path)
				appjson.WriteError(w, err.Error(), http.StatusTooManyRequests)
				return
			}
			slog.Error("rate limiter failed", "error", err, "ip", callerIP, "method", r.Method, "path", r.URL.Path)
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

func (a *AuthMiddleware) Process(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get(AuthKeyHeader)
		if key == "" {
			appjson.WriteError(w, "no api key present", http.StatusUnauthorized)
			return
		}
		sum := sha256.Sum256([]byte(key))
		hash := hex.EncodeToString(sum[:])
		client, err := a.redis.Get(r.Context(), AuthKeyPrefix+hash).Result()
		if err != nil {
			if errors.Is(err, redis.Nil) {
				appjson.WriteError(w, "no api key present", http.StatusUnauthorized)
			} else if errors.Is(err, os.ErrDeadlineExceeded) {
				slog.Error("redis auth deadline exceeded", "error", err)
				appjson.WriteError(w, "internal server error", http.StatusServiceUnavailable)
			} else {
				slog.Error("redis auth error", "error", err)
				appjson.WriteError(w, "internal server error", http.StatusInternalServerError)
			}
			return
		}
		slog.Debug("Client " + client + " authenticated at: " + time.Now().String())
		next.ServeHTTP(w, r)
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
