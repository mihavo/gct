package gate

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/mihavo/gct/internal/config"
	"github.com/redis/go-redis/v9"
)

const testKey = "k_test_0123456789abcdef"

func authHandler(t *testing.T, client *redis.Client) (http.Handler, *string, *int) {
	t.Helper()
	identity := ""
	calls := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		identity, _ = r.Context().Value(ClientId{}).(string)
		w.WriteHeader(http.StatusOK)
	})
	return (&AuthMiddleware{redis: client}).Process(next), &identity, &calls
}

func authRequest(h http.Handler, key string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	if key != "" {
		req.Header.Set(AuthKeyHeader, key)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func seededRedis(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	mr := miniredis.RunT(t)
	if err := mr.Set(AuthKeyPrefix+GenerateKeyHash(testKey), "billing-service"); err != nil {
		t.Fatalf("seeding key: %v", err)
	}
	return mr
}

func TestAuthMiddlewareRejectsARequestWithoutAKey(t *testing.T) {
	mr := seededRedis(t)
	h, _, calls := authHandler(t, newClient(t, mr.Addr()))

	rec := authRequest(h, "")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if got := errorBody(t, rec); got != "no api key present" {
		t.Fatalf("error body = %q, want %q", got, "no api key present")
	}
	if *calls != 0 {
		t.Fatalf("next handler ran %d times, want 0", *calls)
	}
}

func TestAuthMiddlewareRejectsAnUnknownKey(t *testing.T) {
	mr := seededRedis(t)
	h, _, calls := authHandler(t, newClient(t, mr.Addr()))

	rec := authRequest(h, "not-a-real-key")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if got := errorBody(t, rec); got != "invalid api key" {
		t.Fatalf("error body = %q, want %q", got, "invalid api key")
	}
	if *calls != 0 {
		t.Fatalf("next handler ran %d times, want 0", *calls)
	}
}

func TestAuthMiddlewareDoesNotAcceptTheStoredHashAsAKey(t *testing.T) {
	mr := seededRedis(t)
	h, _, calls := authHandler(t, newClient(t, mr.Addr()))

	rec := authRequest(h, GenerateKeyHash(testKey))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if *calls != 0 {
		t.Fatalf("next handler ran %d times, want 0", *calls)
	}
}

func TestAuthMiddlewarePassesAValidKeyWithItsIdentity(t *testing.T) {
	mr := seededRedis(t)
	h, identity, calls := authHandler(t, newClient(t, mr.Addr()))

	rec := authRequest(h, testKey)

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

func TestAuthMiddlewareFailsClosedWhenRedisIsDown(t *testing.T) {
	mr := seededRedis(t)
	client := redis.NewClient(&redis.Options{
		Addr:        mr.Addr(),
		DialTimeout: 200 * time.Millisecond,
		MaxRetries:  -1,
	})
	t.Cleanup(func() { client.Close() })
	h, _, calls := authHandler(t, client)
	mr.Close()

	rec := authRequest(h, testKey)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if *calls != 0 {
		t.Fatalf("next handler ran %d times, want 0", *calls)
	}
}

func TestAuthMiddlewareAnswers503WhenRedisStopsResponding(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { c.Close() })
		}
	}()
	client := redis.NewClient(&redis.Options{
		Addr:        ln.Addr().String(),
		ReadTimeout: 100 * time.Millisecond,
		MaxRetries:  -1,
	})
	t.Cleanup(func() { client.Close() })
	h, _, calls := authHandler(t, client)

	start := time.Now()
	rec := authRequest(h, testKey)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if *calls != 0 {
		t.Fatalf("next handler ran %d times, want 0", *calls)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("answered after %v, want the client read timeout to cut it short", elapsed)
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

func TestProxyReplacesTheClientIdentityAndStripsTheKey(t *testing.T) {
	upstream, seen := newHeaderEchoUpstream(t)
	table, err := buildRoutingTable([]config.RoutesConfig{
		{Name: "auth", Prefix: "/auth", Location: upstream.URL},
	})
	if err != nil {
		t.Fatalf("building routing table: %v", err)
	}
	mr := seededRedis(t)
	h := (&AuthMiddleware{redis: newClient(t, mr.Addr())}).Process(&Handler{Routes: &table})

	req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	req.Header.Set(AuthKeyHeader, testKey)
	req.Header.Set("X-Client-Id", "admin")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := seen.Get("X-Client-Id"); got != "billing-service" {
		t.Errorf("upstream X-Client-Id = %q, want %q", got, "billing-service")
	}
	if got := seen.Get(AuthKeyHeader); got != "" {
		t.Errorf("upstream %s = %q, want it removed", AuthKeyHeader, got)
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
