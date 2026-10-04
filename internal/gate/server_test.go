package gate

import (
	"testing"

	"github.com/mihavo/gct/internal/config"
)

func TestLookup(t *testing.T) {
	table, err := buildRoutingTable([]config.RoutesConfig{
		{Name: "auth", Prefix: "/auth", Location: "http://localhost:8081"},
		{Name: "auth-admin", Prefix: "/auth/admin", Location: "http://localhost:8083"},
		{Name: "service1", Prefix: "/service1", Location: "http://localhost:8082"},
	})
	if err != nil {
		t.Fatalf("building routing table: %v", err)
	}
	g := &Gate{routes: &table}

	tests := []struct {
		name      string
		path      string
		wantRoute string
		wantOK    bool
	}{
		{name: "exact prefix", path: "/auth", wantRoute: "auth", wantOK: true},
		{name: "prefix with trailing slash", path: "/auth/", wantRoute: "auth", wantOK: true},
		{name: "deeper path", path: "/auth/login", wantRoute: "auth", wantOK: true},
		{name: "overlapping exact", path: "/auth/admin", wantRoute: "auth-admin", wantOK: true},
		{name: "overlapping deeper", path: "/auth/admin/users", wantRoute: "auth-admin", wantOK: true},
		{name: "other route", path: "/service1/items/42", wantRoute: "service1", wantOK: true},
		{name: "shared characters without boundary", path: "/authors", wantOK: false},
		{name: "hyphenated sibling", path: "/auth-legacy", wantOK: false},
		{name: "near miss on overlap", path: "/auth/administrators", wantRoute: "auth", wantOK: true},
		{name: "unknown path", path: "/billing", wantOK: false},
		{name: "root", path: "/", wantOK: false},
		{name: "empty path", path: "", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			route, ok := g.Lookup(tt.path)
			if ok != tt.wantOK {
				t.Fatalf("Lookup(%q) ok = %v, want %v", tt.path, ok, tt.wantOK)
			}
			if route.Name != tt.wantRoute {
				t.Errorf("Lookup(%q) route = %q, want %q", tt.path, route.Name, tt.wantRoute)
			}
		})
	}
}
