package gate

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"
	"uuid"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
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

type AuthMiddleware struct {
	keyfnc keyfunc.Keyfunc
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
		scheme, token, found := strings.Cut(r.Header.Get("Authorization"), " ")
		if !found || !strings.EqualFold(scheme, "Bearer") || token == "" {
			appjson.WriteError(w, "no auth token present", http.StatusUnauthorized)
			return
		}
		parsed, err := jwt.Parse(token, a.keyfnc.Keyfunc, jwt.WithValidMethods([]string{jwt.SigningMethodES256.Alg()}), jwt.WithExpirationRequired())
		if err != nil {
			slog.Debug("request denied, token rejected", "error", err, "method", r.Method, "path", r.URL.Path)
			appjson.WriteError(w, "invalid auth token", http.StatusUnauthorized)
			return
		}
		client, err := parsed.Claims.GetSubject()
		if err != nil {
			slog.Warn("request denied, signed token has no usable subject", "error", err, "method", r.Method, "path", r.URL.Path)
			appjson.WriteError(w, "invalid auth token", http.StatusUnauthorized)
			return
		}
		if client == "" {
			slog.Warn("request denied, signed token has no subject", "method", r.Method, "path", r.URL.Path)
			appjson.WriteError(w, "invalid auth token", http.StatusUnauthorized)
			return
		}
		slog.Debug("client authenticated", "client", client)
		ctx := context.WithValue(r.Context(), ClientId{}, client)
		next.ServeHTTP(w, r.WithContext(ctx))
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
