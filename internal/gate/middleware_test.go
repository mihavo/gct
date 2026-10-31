package gate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	appjson "github.com/mihavo/gct/internal/core/json"
)

func limiterHandler(t *testing.T, l *Limiter) (http.Handler, *int) {
	t.Helper()
	calls := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	})
	return (&LimiterMiddleware{limiter: l}).Process(next), &calls
}

func serve(h http.Handler, remoteAddr string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	req.RemoteAddr = remoteAddr
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func errorBody(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body appjson.ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decoding body %q: %v", rec.Body.String(), err)
	}
	return body.Error
}

func TestLimiterMiddlewareFloodFromOneCallerDoesNotAffectAnother(t *testing.T) {
	l, _ := mustLimiter(t, 3, 1, time.Hour)
	h, calls := limiterHandler(t, l)

	for i := range 3 {
		if rec := serve(h, "10.0.0.1:5000"); rec.Code != http.StatusOK {
			t.Fatalf("request %d from the first caller: status = %d, want 200", i, rec.Code)
		}
	}

	rec := serve(h, "10.0.0.1:5000")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("request past the bucket size: status = %d, want 429", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	if got := errorBody(t, rec); got != ErrRateLimited.Error() {
		t.Fatalf("error body = %q, want %q", got, ErrRateLimited.Error())
	}

	if rec := serve(h, "10.0.0.2:5000"); rec.Code != http.StatusOK {
		t.Fatalf("second caller: status = %d, want 200 while the first is limited", rec.Code)
	}
	if *calls != 4 {
		t.Fatalf("next handler ran %d times, want 4 (3 from the first caller, 1 from the second)", *calls)
	}
}

func TestLimiterMiddlewareSharesABucketAcrossPorts(t *testing.T) {
	tests := []struct {
		name  string
		first string
		same  string
		other string
	}{
		{"ipv4", "10.0.0.1:1111", "10.0.0.1:2222", "10.0.0.2:1111"},
		{"ipv6", "[2001:db8::1]:1111", "[2001:db8::1]:2222", "[2001:db8::2]:1111"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l, mr := mustLimiter(t, 1, 1, time.Hour)
			h, _ := limiterHandler(t, l)

			if rec := serve(h, tt.first); rec.Code != http.StatusOK {
				t.Fatalf("first request: status = %d, want 200", rec.Code)
			}
			if rec := serve(h, tt.same); rec.Code != http.StatusTooManyRequests {
				t.Fatalf("same address from another port: status = %d, want 429", rec.Code)
			}
			if rec := serve(h, tt.other); rec.Code != http.StatusOK {
				t.Fatalf("different address: status = %d, want 200", rec.Code)
			}
			if got := mr.Keys(); len(got) != 2 {
				t.Fatalf("keys = %v, want one per address", got)
			}
		})
	}
}

func TestLimiterMiddlewareRejectsAnAddressWithoutAPort(t *testing.T) {
	l, mr := mustLimiter(t, 3, 1, time.Hour)
	h, calls := limiterHandler(t, l)

	rec := serve(h, "not-an-address")

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if got := errorBody(t, rec); got != "unexpected error" {
		t.Fatalf("error body = %q, want the generic message", got)
	}
	if *calls != 0 {
		t.Fatalf("next handler ran %d times, want 0", *calls)
	}
	if got := mr.Keys(); len(got) != 0 {
		t.Fatalf("keys = %v, want none written for an unparsable address", got)
	}
}

func TestLimiterMiddlewareHidesARedisFailure(t *testing.T) {
	l, mr := mustLimiter(t, 3, 1, time.Hour)
	h, calls := limiterHandler(t, l)
	mr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/auth/login", nil).WithContext(ctx)
	req.RemoteAddr = "10.0.0.1:5000"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if got := errorBody(t, rec); got != "unexpected error" {
		t.Fatalf("error body = %q, want the generic message without the redis error", got)
	}
	if *calls != 0 {
		t.Fatalf("next handler ran %d times, want 0 when the limiter cannot decide", *calls)
	}
}
