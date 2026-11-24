package issuer

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	appjson "github.com/mihavo/gct/internal/core/json"
	"github.com/mihavo/gct/internal/gate"
	"github.com/mihavo/gct/internal/keystore"
	"github.com/redis/go-redis/v9"
)

type Handler struct {
	redis *redis.Client
	keys  []keystore.Key
}

func NewHandler(redisClient *redis.Client, keys []keystore.Key) *Handler {
	return &Handler{redis: redisClient, keys: keys}
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
		appjson.WriteError(w, "no api header present", http.StatusUnauthorized)
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

	claims := jwt.RegisteredClaims{
		Issuer:    "gct",
		Subject:   clientName,
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(TokenExpirationInterval)),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	key := h.keys[len(h.keys)-1]
	token.Header["kid"] = key.Kid
	signed, err := token.SignedString(key.Key)

	if err != nil {
		slog.Error("could not sign token", "error", err)
		appjson.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	expiresAt, err := token.Claims.GetExpirationTime()
	if err != nil {
		slog.Error("could not get token expiration date", "error", err)
		appjson.WriteError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	appjson.Write(w, http.StatusCreated, map[string]any{
		"token":     signed,
		"expiresAt": expiresAt})
}

func (h *Handler) jwks(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}
