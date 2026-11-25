package config

import (
	"path/filepath"
	"testing"
)

func TestGateConfigFilesCarryTheJWKSRefreshInterval(t *testing.T) {
	tests := []struct {
		path       string
		want       int
		wantIssuer string
	}{
		{filepath.Join("..", "..", "configs", "gate.yml"), 5, "http://localhost:9090"},
		{filepath.Join("..", "..", "examples", "compose", "gate.yml"), 1, "http://issuer:9090"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got, err := LoadYAML[GateConfig](tt.path)
			if err != nil {
				t.Fatalf("LoadYAML() error = %v", err)
			}
			if got.JWKSRefreshIntervalMinutes != tt.want {
				t.Fatalf("JWKSRefreshIntervalMinutes = %d, want %d", got.JWKSRefreshIntervalMinutes, tt.want)
			}
			if got.IssuerURL != tt.wantIssuer {
				t.Fatalf("IssuerURL = %q, want %q", got.IssuerURL, tt.wantIssuer)
			}
			if got.Server.Port != 8080 || len(got.Routes) != 6 {
				t.Fatalf("rest of the file did not load: port %d, %d routes", got.Server.Port, len(got.Routes))
			}
		})
	}
}

func TestIssuerConfigFilesCarryTheTokenExpirationDelay(t *testing.T) {
	tests := []struct {
		path string
		want int
	}{
		{filepath.Join("..", "..", "configs", "issuer.yml"), 60},
		{filepath.Join("..", "..", "examples", "compose", "issuer.yml"), 1},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got, err := LoadYAML[IssuerConfig](tt.path)
			if err != nil {
				t.Fatalf("LoadYAML() error = %v", err)
			}
			if got.TokenExpirationDelayMinutes != tt.want {
				t.Fatalf("TokenExpirationDelayMinutes = %d, want %d", got.TokenExpirationDelayMinutes, tt.want)
			}
			if got.Server.Port != 9090 {
				t.Fatalf("rest of the file did not load: port %d", got.Server.Port)
			}
		})
	}
}
