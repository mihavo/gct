package issuer

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"

	appjson "github.com/mihavo/gct/internal/core/json"
	"github.com/mihavo/gct/internal/gate"
	"github.com/redis/go-redis/v9"
)

type Handler struct {
	redis *redis.Client
}

func NewHandler(redisClient *redis.Client) *Handler {
	return &Handler{redis: redisClient}
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /token", h.token)
	mux.HandleFunc("GET /jwks", h.jwks)
	return mux
}

func (h *Handler) token(w http.ResponseWriter, r *http.Request) {
	header := r.Header.Get(gate.AuthKeyHeader)

	if header == "" {
		appjson.WriteError(w, "no api header present", http.StatusBadRequest)
		return
	}
	clientName, err := h.redis.Get(r.Context(), gate.AuthKeyPrefix+gate.GenerateKeyHash(header)).Result()

	if err != nil {
		if errors.Is(err, redis.Nil) {
			appjson.WriteError(w, "invalid api key", http.StatusUnauthorized)
		} else if errors.Is(err, os.ErrDeadlineExceeded) {
			slog.Error("redis read deadline exceeded", "error", err)
			appjson.WriteError(w, "internal server error", http.StatusServiceUnavailable)
		} else if errors.Is(err, context.DeadlineExceeded) {
			slog.Error("redis connection timeout", "error", err)
			appjson.WriteError(w, "internal server error", http.StatusServiceUnavailable)
		} else {
			slog.Error("redis lookup error", "error", err)
			appjson.WriteError(w, "internal server error", http.StatusInternalServerError)
		}
		return
	}

}

func (h *Handler) jwks(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}
