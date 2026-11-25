package gate

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
	"github.com/mihavo/gct/internal/config"
)

const (
	testKey = "k_test_0123456789abcdef"
	testKid = "key-1"
)

func newTestKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}
	return key
}

func jwkSetJSON(t *testing.T, keys map[string]*ecdsa.PublicKey) json.RawMessage {
	t.Helper()
	entries := make([]map[string]string, 0, len(keys))
	for kid, pub := range keys {
		raw, err := pub.Bytes()
		if err != nil {
			t.Fatalf("encoding public key: %v", err)
		}
		size := (pub.Curve.Params().BitSize + 7) / 8
		entries = append(entries, map[string]string{
			"kty": "EC",
			"crv": pub.Curve.Params().Name,
			"kid": kid,
			"x":   base64.RawURLEncoding.EncodeToString(raw[1 : 1+size]),
			"y":   base64.RawURLEncoding.EncodeToString(raw[1+size:]),
		})
	}
	out, err := json.Marshal(map[string]any{"keys": entries})
	if err != nil {
		t.Fatalf("encoding key set: %v", err)
	}
	return out
}

func newTestAuth(t *testing.T, keys map[string]*ecdsa.PublicKey) *AuthMiddleware {
	t.Helper()
	kf, err := keyfunc.NewJWKSetJSON(jwkSetJSON(t, keys))
	if err != nil {
		t.Fatalf("building keyfunc: %v", err)
	}
	return &AuthMiddleware{keyfnc: kf}
}

func newP256Auth(t *testing.T, key *ecdsa.PrivateKey) *AuthMiddleware {
	t.Helper()
	return newTestAuth(t, map[string]*ecdsa.PublicKey{testKid: &key.PublicKey})
}

func signToken(t *testing.T, method jwt.SigningMethod, key any, kid string, claims jwt.Claims) string {
	t.Helper()
	token := jwt.NewWithClaims(method, claims)
	token.Header["kid"] = kid
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("signing token: %v", err)
	}
	return signed
}

func validClaims(sub string) jwt.MapClaims {
	return jwt.MapClaims{
		"sub": sub,
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(10 * time.Minute).Unix(),
	}
}

func authHandler(t *testing.T, mw *AuthMiddleware) (http.Handler, *string, *int) {
	t.Helper()
	identity := ""
	calls := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		identity, _ = r.Context().Value(ClientId{}).(string)
		w.WriteHeader(http.StatusOK)
	})
	return mw.Process(next), &identity, &calls
}

func authRequest(h http.Handler, authorization string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAuthMiddlewareRejectsARequestItCannotTrust(t *testing.T) {
	key := newTestKey(t)
	otherKey := newTestKey(t)
	p384Key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatalf("generating P-384 key: %v", err)
	}
	keys := map[string]*ecdsa.PublicKey{testKid: &key.PublicKey, "key-384": &p384Key.PublicKey}

	tests := []struct {
		name          string
		authorization string
		wantBody      string
	}{
		{"no header", "", "no auth token present"},
		{"another scheme", "Basic dXNlcjpwYXNz", "no auth token present"},
		{"scheme without a token", "Bearer ", "no auth token present"},
		{"not a jwt", "Bearer not-a-jwt", "invalid auth token"},
		{
			"expired",
			"Bearer " + signToken(t, jwt.SigningMethodES256, key, testKid, jwt.MapClaims{
				"sub": "billing-service",
				"exp": time.Now().Add(-time.Minute).Unix(),
			}),
			"invalid auth token",
		},
		{
			"no expiry",
			"Bearer " + signToken(t, jwt.SigningMethodES256, key, testKid, jwt.MapClaims{"sub": "billing-service"}),
			"invalid auth token",
		},
		{
			"signed by another key under a known kid",
			"Bearer " + signToken(t, jwt.SigningMethodES256, otherKey, testKid, validClaims("billing-service")),
			"invalid auth token",
		},
		{
			"unknown kid",
			"Bearer " + signToken(t, jwt.SigningMethodES256, key, "key-2", validClaims("billing-service")),
			"invalid auth token",
		},
		{
			"symmetric algorithm",
			"Bearer " + signToken(t, jwt.SigningMethodHS256, []byte("secret"), testKid, validClaims("billing-service")),
			"invalid auth token",
		},
		{
			"another elliptic curve algorithm",
			"Bearer " + signToken(t, jwt.SigningMethodES384, p384Key, "key-384", validClaims("billing-service")),
			"invalid auth token",
		},
		{
			"no subject",
			"Bearer " + signToken(t, jwt.SigningMethodES256, key, testKid, jwt.MapClaims{
				"exp": time.Now().Add(10 * time.Minute).Unix(),
			}),
			"invalid auth token",
		},
		{
			"empty subject",
			"Bearer " + signToken(t, jwt.SigningMethodES256, key, testKid, validClaims("")),
			"invalid auth token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, _, calls := authHandler(t, newTestAuth(t, keys))

			rec := authRequest(h, tt.authorization)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", rec.Code)
			}
			if got := errorBody(t, rec); got != tt.wantBody {
				t.Fatalf("error body = %q, want %q", got, tt.wantBody)
			}
			if *calls != 0 {
				t.Fatalf("next handler ran %d times, want 0", *calls)
			}
		})
	}
}

func TestAuthMiddlewarePassesAValidTokenWithItsSubject(t *testing.T) {
	key := newTestKey(t)
	h, identity, calls := authHandler(t, newP256Auth(t, key))

	rec := authRequest(h, "Bearer "+signToken(t, jwt.SigningMethodES256, key, testKid, validClaims("billing-service")))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if *calls != 1 {
		t.Fatalf("next handler ran %d times, want 1", *calls)
	}
	if *identity != "billing-service" {
		t.Fatalf("identity in context = %q, want %q", *identity, "billing-service")
	}
}

func TestAuthMiddlewareIgnoresTheCaseOfTheScheme(t *testing.T) {
	key := newTestKey(t)
	h, _, calls := authHandler(t, newP256Auth(t, key))

	rec := authRequest(h, "bearer "+signToken(t, jwt.SigningMethodES256, key, testKid, validClaims("billing-service")))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if *calls != 1 {
		t.Fatalf("next handler ran %d times, want 1", *calls)
	}
}

func TestAuthMiddlewareDoesNotLookAtTheAPIKeyHeader(t *testing.T) {
	key := newTestKey(t)
	h, _, calls := authHandler(t, newP256Auth(t, key))

	req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	req.Header.Set(AuthKeyHeader, testKey)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if *calls != 0 {
		t.Fatalf("next handler ran %d times, want 0", *calls)
	}
}

func newHeaderEchoUpstream(t *testing.T) (*httptest.Server, *http.Header) {
	t.Helper()
	seen := http.Header{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func TestProxyReplacesTheClientIdentityWithTheTokenSubject(t *testing.T) {
	upstream, seen := newHeaderEchoUpstream(t)
	table, err := buildRoutingTable([]config.RoutesConfig{
		{Name: "auth", Prefix: "/auth", Location: upstream.URL},
	})
	if err != nil {
		t.Fatalf("building routing table: %v", err)
	}
	key := newTestKey(t)
	h := newP256Auth(t, key).Process(&Handler{Routes: &table})

	req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	req.Header.Set("Authorization", "Bearer "+signToken(t, jwt.SigningMethodES256, key, testKid, validClaims("billing-service")))
	req.Header.Set("X-Client-Id", "admin")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := seen.Get("X-Client-Id"); got != "billing-service" {
		t.Errorf("upstream X-Client-Id = %q, want %q", got, "billing-service")
	}
	if got := seen.Get("Authorization"); got != "" {
		t.Errorf("upstream Authorization = %q, want it removed", got)
	}
}

func TestProxyDropsAClientSuppliedIdentityWhenNoneIsAuthenticated(t *testing.T) {
	upstream, seen := newHeaderEchoUpstream(t)
	table, err := buildRoutingTable([]config.RoutesConfig{
		{Name: "auth", Prefix: "/auth", Location: upstream.URL},
	})
	if err != nil {
		t.Fatalf("building routing table: %v", err)
	}
	h := &Handler{Routes: &table}

	req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	req.Header.Set("X-Client-Id", "admin")
	req.Header.Set(AuthKeyHeader, testKey)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := seen.Get("X-Client-Id"); got != "" {
		t.Errorf("upstream X-Client-Id = %q, want it removed", got)
	}
	if got := seen.Get(AuthKeyHeader); got != "" {
		t.Errorf("upstream %s = %q, want it removed", AuthKeyHeader, got)
	}
}
