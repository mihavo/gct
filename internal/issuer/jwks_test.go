package issuer

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/mihavo/gct/internal/gate"
	"github.com/mihavo/gct/internal/keystore"
)

func newTestKey(t *testing.T) keystore.Key {
	t.Helper()
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}
	kid, err := keystore.KeyID(&private.PublicKey)
	if err != nil {
		t.Fatalf("computing kid: %v", err)
	}
	return keystore.Key{Kid: kid, Key: private}
}

func getJWKS(h http.Handler) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/jwks", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decodeJWKS(t *testing.T, rec *httptest.ResponseRecorder) []JWK {
	t.Helper()
	var body struct {
		Keys []JWK `json:"keys"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding body %q: %v", rec.Body.String(), err)
	}
	return body.Keys
}

func rebuildPublicKey(t *testing.T, jwk JWK) *ecdsa.PublicKey {
	t.Helper()
	x, err := base64.RawURLEncoding.DecodeString(jwk.X)
	if err != nil {
		t.Fatalf("x is not unpadded base64url: %v", err)
	}
	y, err := base64.RawURLEncoding.DecodeString(jwk.Y)
	if err != nil {
		t.Fatalf("y is not unpadded base64url: %v", err)
	}
	point := append([]byte{0x04}, append(x, y...)...)
	pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), point)
	if err != nil {
		t.Fatalf("x and y are not a point on P-256: %v", err)
	}
	return pub
}

func TestJWKSPublishesTheKeyInJWKForm(t *testing.T) {
	keys := []keystore.Key{newTestKey(t)}
	h := NewHandler(nil, keys, testDelay).Routes()

	rec := getJWKS(h)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	got := decodeJWKS(t, rec)
	if len(got) != 1 {
		t.Fatalf("got %d keys, want 1", len(got))
	}
	jwk := got[0]
	if jwk.Kty != "EC" || jwk.Crv != "P-256" || jwk.Kid != keys[0].Kid {
		t.Fatalf("kty, crv, kid = %q, %q, %q", jwk.Kty, jwk.Crv, jwk.Kid)
	}
	if !rebuildPublicKey(t, jwk).Equal(&keys[0].Key.PublicKey) {
		t.Fatal("x and y do not rebuild the signing key's public half")
	}
}

func TestJWKSCoordinatesAreFullLength(t *testing.T) {
	var key keystore.Key
	found := false
	for range 20000 {
		key = newTestKey(t)
		point, err := key.Key.PublicKey.Bytes()
		if err != nil {
			t.Fatalf("encoding key: %v", err)
		}
		if point[1] == 0 || point[33] == 0 {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("no key with a leading zero byte found in 20000 tries")
	}
	h := NewHandler(nil, []keystore.Key{key}, testDelay).Routes()

	jwk := decodeJWKS(t, getJWKS(h))[0]

	for name, value := range map[string]string{"x": jwk.X, "y": jwk.Y} {
		raw, err := base64.RawURLEncoding.DecodeString(value)
		if err != nil {
			t.Fatalf("%s is not unpadded base64url: %v", name, err)
		}
		if len(raw) != 32 {
			t.Fatalf("%s is %d bytes, want 32", name, len(raw))
		}
	}
	if !rebuildPublicKey(t, jwk).Equal(&key.Key.PublicKey) {
		t.Fatal("x and y do not rebuild the signing key's public half")
	}
}

func TestJWKSPublishesEveryKey(t *testing.T) {
	keys := []keystore.Key{newTestKey(t), newTestKey(t)}
	h := NewHandler(nil, keys, testDelay).Routes()

	got := decodeJWKS(t, getJWKS(h))

	if len(got) != 2 {
		t.Fatalf("got %d keys, want 2", len(got))
	}
	for i, key := range keys {
		if got[i].Kid != key.Kid {
			t.Fatalf("entry %d kid = %q, want %q", i, got[i].Kid, key.Kid)
		}
		if !rebuildPublicKey(t, got[i]).Equal(&key.Key.PublicKey) {
			t.Fatalf("entry %d does not rebuild key %q", i, key.Kid)
		}
	}
}

func TestJWKSOnlyPublishesTheFiveMembers(t *testing.T) {
	h := NewHandler(nil, []keystore.Key{newTestKey(t)}, testDelay).Routes()

	var body struct {
		Keys []map[string]any `json:"keys"`
	}
	if err := json.Unmarshal(getJWKS(h).Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding body: %v", err)
	}

	var members []string
	for name := range body.Keys[0] {
		members = append(members, name)
	}
	sort.Strings(members)
	want := []string{"crv", "kid", "kty", "x", "y"}
	if len(members) != len(want) {
		t.Fatalf("members = %v, want %v", members, want)
	}
	for i := range want {
		if members[i] != want[i] {
			t.Fatalf("members = %v, want %v", members, want)
		}
	}
}

func TestJWKSNeedsNoAPIKeyAndNoRedis(t *testing.T) {
	h := NewHandler(nil, []keystore.Key{newTestKey(t)}, testDelay).Routes()

	req := httptest.NewRequest(http.MethodGet, "/jwks", nil)
	if req.Header.Get(gate.AuthKeyHeader) != "" {
		t.Fatal("test request must not carry an api key")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestJWKSOnlyAcceptsGet(t *testing.T) {
	h := NewHandler(nil, []keystore.Key{newTestKey(t)}, testDelay).Routes()

	req := httptest.NewRequest(http.MethodPost, "/jwks", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestTokenVerifiesAgainstTheKeyFoundInTheKeySetByKid(t *testing.T) {
	mr := seededRedis(t)
	keys := keysAged(t, 72*time.Hour, 5*time.Minute)
	h := NewHandler(redisClient(t, mr.Addr()), keys, testDelay).Routes()

	rec := postToken(h, testAPIKey)
	var issued struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &issued); err != nil {
		t.Fatalf("decoding token body: %v", err)
	}
	published := decodeJWKS(t, getJWKS(h))

	parsed, _, err := parseToken(t, issued.Token, keys[0])
	if err != nil {
		t.Fatalf("parsing token: %v", err)
	}
	kid, _ := parsed.Header["kid"].(string)
	var match *JWK
	for i := range published {
		if published[i].Kid == kid {
			match = &published[i]
		}
	}
	if match == nil {
		t.Fatalf("kid %q of the token is not in the published key set", kid)
	}
	pub := rebuildPublicKey(t, *match)
	if !pub.Equal(&keys[0].Key.PublicKey) {
		t.Fatal("the published key for the token's kid is not the signing key")
	}
}
