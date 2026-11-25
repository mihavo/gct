package gate

import (
	"context"
	"crypto/ecdsa"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/mihavo/gct/internal/config"
)

func gateConfigFor(issuerURL, upstreamURL string) config.GateConfig {
	return config.GateConfig{
		Server:                     config.ServerConfig{ListenAddress: "127.0.0.1", Port: 0},
		Limiter:                    limiterConfig(10, 10, time.Second),
		JWKSRefreshIntervalMinutes: 1,
		IssuerURL:                  issuerURL,
		Routes:                     []config.RoutesConfig{{Name: "auth", Prefix: "/auth", Location: upstreamURL}},
	}
}

func newJWKSServer(t *testing.T, keys map[string]*ecdsa.PublicKey) (*httptest.Server, *int) {
	t.Helper()
	hits := 0
	body := jwkSetJSON(t, keys)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != JWKSPath {
			http.NotFound(w, r)
			return
		}
		hits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func TestNewGateFetchesTheKeySetFromTheIssuer(t *testing.T) {
	key := newTestKey(t)
	issuer, hits := newJWKSServer(t, map[string]*ecdsa.PublicKey{testKid: &key.PublicKey})
	upstream, seen := newHeaderEchoUpstream(t)
	mr := miniredis.RunT(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	g, err := NewGate(ctx, newClient(t, mr.Addr()), gateConfigFor(issuer.URL+"/", upstream.URL))
	if err != nil {
		t.Fatalf("NewGate() error = %v", err)
	}
	if *hits != 1 {
		t.Fatalf("issuer served %d key set requests during startup, want 1", *hits)
	}

	good := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	good.Header.Set("Authorization", "Bearer "+signToken(t, jwt.SigningMethodES256, key, testKid, validClaims("billing-service")))
	rec := httptest.NewRecorder()
	g.Server.Handler.ServeHTTP(rec, good)
	if rec.Code != http.StatusOK {
		t.Fatalf("status for a token from the issuer's key = %d, want 200", rec.Code)
	}
	if got := seen.Get("X-Client-Id"); got != "billing-service" {
		t.Errorf("upstream X-Client-Id = %q, want %q", got, "billing-service")
	}

	forged := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	forged.Header.Set("Authorization", "Bearer "+signToken(t, jwt.SigningMethodES256, newTestKey(t), testKid, validClaims("billing-service")))
	*seen = http.Header{}
	rec = httptest.NewRecorder()
	g.Server.Handler.ServeHTTP(rec, forged)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status for a token from another key = %d, want 401", rec.Code)
	}
	if got := seen.Get("X-Client-Id"); got != "" {
		t.Errorf("a rejected token reached the upstream with X-Client-Id %q", got)
	}
}

func TestNewGateRefusesToStartWhenTheIssuerIsDown(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	deadURL := "http://" + ln.Addr().String()
	ln.Close()
	upstream, _ := newHeaderEchoUpstream(t)
	mr := miniredis.RunT(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	g, err := NewGate(ctx, newClient(t, mr.Addr()), gateConfigFor(deadURL, upstream.URL))

	if err == nil {
		t.Fatal("NewGate() error = nil, want an error when the first key set fetch fails")
	}
	if g != nil {
		t.Fatalf("NewGate() returned a gate together with the error %v", err)
	}
}
