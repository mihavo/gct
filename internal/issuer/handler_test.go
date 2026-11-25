package issuer

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/mihavo/gct/internal/gate"
	"github.com/mihavo/gct/internal/keystore"
	"github.com/redis/go-redis/v9"
)

const testAPIKey = "k_test_0123456789abcdef"

func seededRedis(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	mr := miniredis.RunT(t)
	if err := mr.Set(gate.AuthKeyPrefix+gate.GenerateKeyHash(testAPIKey), "billing-service"); err != nil {
		t.Fatalf("seeding key: %v", err)
	}
	return mr
}

func redisClient(t *testing.T, addr string) *redis.Client {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { client.Close() })
	return client
}

func postToken(h http.Handler, apiKey string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/token", nil)
	if apiKey != "" {
		req.Header.Set(gate.AuthKeyHeader, apiKey)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func errorBody(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding error body %q: %v", rec.Body.String(), err)
	}
	return body.Error
}

func assertNoToken(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding body %q: %v", rec.Body.String(), err)
	}
	if _, ok := body["token"]; ok {
		t.Fatalf("response contains a token: %s", rec.Body.String())
	}
}

func parseToken(t *testing.T, raw string, key keystore.Key) (*jwt.Token, *jwt.RegisteredClaims, error) {
	t.Helper()
	claims := &jwt.RegisteredClaims{}
	token, err := jwt.ParseWithClaims(raw, claims, func(*jwt.Token) (any, error) {
		return &key.Key.PublicKey, nil
	}, jwt.WithValidMethods([]string{"ES256"}), jwt.WithIssuer("gct"), jwt.WithExpirationRequired())
	return token, claims, err
}

func TestTokenIssuesASignedTokenForAValidKey(t *testing.T) {
	mr := seededRedis(t)
	keys := testKeys(t)
	h := NewHandler(redisClient(t, mr.Addr()), keys, testDelay).Routes()

	rec := postToken(h, testAPIKey)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	var body struct {
		Token     string  `json:"token"`
		ExpiresAt float64 `json:"expiresAt"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding body %q: %v", rec.Body.String(), err)
	}
	if body.Token == "" {
		t.Fatalf("response has no token: %s", rec.Body.String())
	}

	token, claims, err := parseToken(t, body.Token, keys[0])
	if err != nil {
		t.Fatalf("token does not verify against the issuer's public key: %v", err)
	}
	if got := token.Header["kid"]; got != keys[0].Kid {
		t.Fatalf("header kid = %v, want %q", got, keys[0].Kid)
	}
	if got := token.Header["alg"]; got != "ES256" {
		t.Fatalf("header alg = %v, want ES256", got)
	}
	if claims.Subject != "billing-service" {
		t.Fatalf("sub = %q, want %q", claims.Subject, "billing-service")
	}
	if claims.IssuedAt == nil || claims.ExpiresAt == nil {
		t.Fatalf("iat or exp is missing: %+v", claims)
	}
	lifetime := claims.ExpiresAt.Sub(claims.IssuedAt.Time)
	if diff := lifetime - TokenExpirationInterval; diff < -time.Second || diff > time.Second {
		t.Fatalf("exp - iat = %v, want %v (within 1s)", lifetime, TokenExpirationInterval)
	}
	if int64(body.ExpiresAt) != claims.ExpiresAt.Unix() {
		t.Fatalf("response expiresAt = %v, but the token's exp is %d", body.ExpiresAt, claims.ExpiresAt.Unix())
	}
}

func TestTokenDoesNotVerifyWithAnotherKey(t *testing.T) {
	mr := seededRedis(t)
	keys := testKeys(t)
	other := testKeys(t)
	h := NewHandler(redisClient(t, mr.Addr()), keys, testDelay).Routes()

	rec := postToken(h, testAPIKey)
	var body struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Token == "" {
		t.Fatalf("no token in response %q: %v", rec.Body.String(), err)
	}

	if _, _, err := parseToken(t, body.Token, other[0]); err == nil {
		t.Fatalf("token verified with a key that did not sign it")
	}
}

func TestTokenRejectsARequestWithoutAKey(t *testing.T) {
	mr := seededRedis(t)
	h := NewHandler(redisClient(t, mr.Addr()), testKeys(t), testDelay).Routes()

	rec := postToken(h, "")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if got := errorBody(t, rec); got != "no api header present" {
		t.Fatalf("error body = %q, want %q", got, "no api header present")
	}
}

func TestTokenRejectsAnUnknownKey(t *testing.T) {
	mr := seededRedis(t)
	h := NewHandler(redisClient(t, mr.Addr()), testKeys(t), testDelay).Routes()

	rec := postToken(h, "not-a-real-key")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if got := errorBody(t, rec); got != "invalid api key" {
		t.Fatalf("error body = %q, want %q", got, "invalid api key")
	}
	assertNoToken(t, rec)
}

func TestTokenDoesNotAcceptTheStoredHashAsAKey(t *testing.T) {
	mr := seededRedis(t)
	h := NewHandler(redisClient(t, mr.Addr()), testKeys(t), testDelay).Routes()

	rec := postToken(h, gate.GenerateKeyHash(testAPIKey))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	assertNoToken(t, rec)
}

func TestTokenFailsClosedWhenRedisIsDown(t *testing.T) {
	mr := seededRedis(t)
	client := redis.NewClient(&redis.Options{
		Addr:        mr.Addr(),
		DialTimeout: 200 * time.Millisecond,
		MaxRetries:  -1,
	})
	t.Cleanup(func() { client.Close() })
	h := NewHandler(client, testKeys(t), testDelay).Routes()
	mr.Close()

	rec := postToken(h, testAPIKey)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	assertNoToken(t, rec)
}

func TestTokenAnswers503WhenRedisStopsResponding(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { conn.Close() })
		}
	}()
	client := redis.NewClient(&redis.Options{
		Addr:        ln.Addr().String(),
		ReadTimeout: 100 * time.Millisecond,
		MaxRetries:  -1,
	})
	t.Cleanup(func() { client.Close() })
	h := NewHandler(client, testKeys(t), testDelay).Routes()

	start := time.Now()
	rec := postToken(h, testAPIKey)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	assertNoToken(t, rec)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("answered after %v, want the client read timeout to cut it short", elapsed)
	}
}

func TestTokenOnlyAcceptsPost(t *testing.T) {
	mr := seededRedis(t)
	h := NewHandler(redisClient(t, mr.Addr()), testKeys(t), testDelay).Routes()

	req := httptest.NewRequest(http.MethodGet, "/token", nil)
	req.Header.Set(gate.AuthKeyHeader, testAPIKey)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}
