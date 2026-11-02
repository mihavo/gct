package gate

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mihavo/gct/internal/config"
)

func newEchoUpstream(t *testing.T, name string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Seen-Path", r.URL.Path)
		w.Header().Set("X-Seen-Escaped-Path", r.URL.EscapedPath())
		w.Header().Set("X-Seen-Query", r.URL.RawQuery)
		w.Header().Set("X-Seen-Prefix", r.Header.Get("X-Forwarded-Prefix"))
		w.Header().Set("X-Seen-Forwarded-For", r.Header.Get("X-Forwarded-For"))
		w.Header().Set("X-Seen-Request-Id", r.Header.Get("X-Request-Id"))
		io.WriteString(w, name)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestForwardKeepsEncodedSlashes(t *testing.T) {
	auth := newEchoUpstream(t, "auth")
	service1 := newEchoUpstream(t, "service1")

	table, err := buildRoutingTable([]config.RoutesConfig{
		{Name: "auth", Prefix: "/auth", Location: auth.URL},
		{Name: "service1", Prefix: "/service1", Location: service1.URL + "/v1"},
	})
	if err != nil {
		t.Fatalf("building routing table: %v", err)
	}
	h := (&LoggingMiddleware{}).Process(&Handler{Routes: &table})

	tests := []struct {
		name        string
		target      string
		wantPath    string
		wantEscaped string
	}{
		{"encoded slash in a segment", "/auth/files/a%2Fb", "/files/a/b", "/files/a%2Fb"},
		{"encoded slash with an upstream base path", "/service1/items/a%2Fb", "/v1/items/a/b", "/v1/items/a%2Fb"},
		{"other escapes survive", "/auth/files/a%20b", "/files/a b", "/files/a%20b"},
		{"plain path unchanged", "/auth/login", "/login", "/login"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()

			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.target, nil))

			res := rec.Result()
			if res.StatusCode != http.StatusOK {
				t.Fatalf("GET %s status = %d, want 200", tt.target, res.StatusCode)
			}
			if got := res.Header.Get("X-Seen-Path"); got != tt.wantPath {
				t.Errorf("GET %s upstream path = %q, want %q", tt.target, got, tt.wantPath)
			}
			if got := res.Header.Get("X-Seen-Escaped-Path"); got != tt.wantEscaped {
				t.Errorf("GET %s upstream escaped path = %q, want %q", tt.target, got, tt.wantEscaped)
			}
		})
	}
}

func TestForward(t *testing.T) {
	auth := newEchoUpstream(t, "auth")
	service1 := newEchoUpstream(t, "service1")

	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()

	table, err := buildRoutingTable([]config.RoutesConfig{
		{Name: "auth", Prefix: "/auth", Location: auth.URL},
		{Name: "service1", Prefix: "/service1", Location: service1.URL + "/v1"},
		{Name: "dead", Prefix: "/dead", Location: deadURL},
	})
	if err != nil {
		t.Fatalf("building routing table: %v", err)
	}
	logger := LoggingMiddleware{}
	h := logger.Process(&Handler{Routes: &table})

	tests := []struct {
		name       string
		target     string
		header     map[string]string
		wantStatus int
		wantBody   string
		wantPath   string
		wantQuery  string
		wantPrefix string
	}{
		{name: "deeper path", target: "/auth/login", wantStatus: http.StatusOK, wantBody: "auth", wantPath: "/login", wantPrefix: "/auth"},
		{name: "exact prefix", target: "/auth", wantStatus: http.StatusOK, wantBody: "auth", wantPath: "/", wantPrefix: "/auth"},
		{name: "prefix with trailing slash", target: "/auth/", wantStatus: http.StatusOK, wantBody: "auth", wantPath: "/", wantPrefix: "/auth"},
		{name: "query kept", target: "/auth/login?user=1&next=%2Fhome", wantStatus: http.StatusOK, wantBody: "auth", wantPath: "/login", wantQuery: "user=1&next=%2Fhome", wantPrefix: "/auth"},
		{name: "upstream base path", target: "/service1/items/42", wantStatus: http.StatusOK, wantBody: "service1", wantPath: "/v1/items/42", wantPrefix: "/service1"},
		{name: "client prefix header replaced", target: "/auth/login", header: map[string]string{"X-Forwarded-Prefix": "/evil"}, wantStatus: http.StatusOK, wantBody: "auth", wantPath: "/login", wantPrefix: "/auth"},
		{name: "unknown path", target: "/billing", wantStatus: http.StatusNotFound},
		{name: "upstream down", target: "/dead/ping", wantStatus: http.StatusBadGateway},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.target, nil)
			for k, v := range tt.header {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()

			h.ServeHTTP(rec, req)

			res := rec.Result()
			if res.StatusCode != tt.wantStatus {
				t.Fatalf("GET %s status = %d, want %d", tt.target, res.StatusCode, tt.wantStatus)
			}
			if tt.wantStatus != http.StatusOK {
				return
			}
			if got := rec.Body.String(); got != tt.wantBody {
				t.Errorf("GET %s reached %q, want %q", tt.target, got, tt.wantBody)
			}
			if got := res.Header.Get("X-Seen-Path"); got != tt.wantPath {
				t.Errorf("GET %s upstream path = %q, want %q", tt.target, got, tt.wantPath)
			}
			if got := res.Header.Get("X-Seen-Query"); got != tt.wantQuery {
				t.Errorf("GET %s upstream query = %q, want %q", tt.target, got, tt.wantQuery)
			}
			if got := res.Header.Get("X-Seen-Prefix"); got != tt.wantPrefix {
				t.Errorf("GET %s upstream X-Forwarded-Prefix = %q, want %q", tt.target, got, tt.wantPrefix)
			}
			if got := res.Header.Get("X-Seen-Forwarded-For"); got == "" {
				t.Errorf("GET %s upstream X-Forwarded-For is empty", tt.target)
			}
			if got := res.Header.Get("X-Seen-Request-Id"); got == "" {
				t.Errorf("GET %s upstream X-Request-Id is empty", tt.target)
			}
		})
	}
}
