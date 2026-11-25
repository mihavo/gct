package issuer

import (
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	appjson "github.com/mihavo/gct/internal/core/json"
	"github.com/mihavo/gct/internal/gate"
	"github.com/mihavo/gct/internal/keystore"
	"github.com/redis/go-redis/v9"
)

type Handler struct {
	redis                *redis.Client
	keys                 []keystore.Key
	TokenExpirationDelay time.Duration
	mux                  sync.RWMutex
}

type JWK struct {
	Kty string `json:"kty"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
	Kid string `json:"kid"`
}

func NewHandler(redisClient *redis.Client, keys []keystore.Key, tokenExpirationDelay time.Duration) *Handler {
	return &Handler{redis: redisClient, keys: slices.Clone(keys), TokenExpirationDelay: tokenExpirationDelay}
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
	var key keystore.Key

	h.mux.Lock()
	now := time.Now()
	usable := make([]keystore.Key, 0, len(h.keys))
	for _, candidate := range h.keys {
		if candidate.LastModified.Add(h.TokenExpirationDelay).Before(now) {
			usable = append(usable, candidate)
		}
	}
	if len(usable) == 0 {
		key = slices.MinFunc(h.keys, func(a, b keystore.Key) int {
			return a.LastModified.Compare(b.LastModified)
		})
	} else {
		key = slices.MaxFunc(usable, func(a, b keystore.Key) int {
			return a.LastModified.Compare(b.LastModified)
		})
		if len(usable) > 1 && key.LastModified.Add(h.TokenExpirationDelay).Add(TokenExpirationInterval).Add(KeyRemovalErrorMargin).Before(now) {
			h.keys = slices.DeleteFunc(h.keys, func(candidate keystore.Key) bool {
				return candidate.Kid != key.Kid && candidate.LastModified.Add(h.TokenExpirationDelay).Before(now)
			})
		}
	}
	h.mux.Unlock()

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
	h.mux.RLock()
	pubs := make([]JWK, 0, len(h.keys))

	for _, key := range h.keys {
		point, err := key.Key.PublicKey.Bytes()
		if err != nil {
			slog.Error("could not encode public key", "kid", key.Kid, "error", err)
			appjson.WriteError(w, "internal server error", http.StatusInternalServerError)
			h.mux.RUnlock()
			return
		}
		size := (key.Key.Curve.Params().BitSize + 7) / 8
		jwk := JWK{
			Kty: "EC",
			Crv: key.Key.Curve.Params().Name,
			X:   base64.RawURLEncoding.EncodeToString(point[1 : 1+size]),
			Y:   base64.RawURLEncoding.EncodeToString(point[1+size:]),
			Kid: key.Kid,
		}
		pubs = append(pubs, jwk)
	}
	h.mux.RUnlock()
	appjson.Write(w, http.StatusOK, map[string]any{
		"keys": pubs,
	})
}
