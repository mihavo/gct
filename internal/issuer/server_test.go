package issuer

import (
	"context"
	"testing"

	"github.com/mihavo/gct/internal/config"
	"github.com/redis/go-redis/v9"
)

func TestNewIssuerConfig(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "localhost:0"})
	t.Cleanup(func() { client.Close() })

	valid := config.IssuerConfig{Server: config.ServerConfig{ListenAddress: "localhost", Port: 9090}}

	tests := []struct {
		name    string
		client  *redis.Client
		cfg     config.IssuerConfig
		wantErr bool
	}{
		{"valid", client, valid, false},
		{"nil redis", nil, valid, true},
		{"empty listen address", client, config.IssuerConfig{Server: config.ServerConfig{Port: 9090}}, true},
		{"zero value config", client, config.IssuerConfig{}, true},
		{"port zero", client, config.IssuerConfig{Server: config.ServerConfig{ListenAddress: "localhost"}}, true},
		{"negative port", client, config.IssuerConfig{Server: config.ServerConfig{ListenAddress: "localhost", Port: -1}}, true},
		{"port above range", client, config.IssuerConfig{Server: config.ServerConfig{ListenAddress: "localhost", Port: 65536}}, true},
		{"lowest port", client, config.IssuerConfig{Server: config.ServerConfig{ListenAddress: "localhost", Port: 1}}, false},
		{"highest port", client, config.IssuerConfig{Server: config.ServerConfig{ListenAddress: "localhost", Port: 65535}}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewIssuer(context.Background(), tt.client, tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Fatalf("NewIssuer() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && got != nil {
				t.Fatalf("NewIssuer() returned an issuer together with an error")
			}
			if !tt.wantErr && (got == nil || got.Server == nil || got.Server.Handler == nil) {
				t.Fatalf("NewIssuer() returned an incomplete issuer: %+v", got)
			}
		})
	}
}

func TestNewIssuerAddr(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "localhost:0"})
	t.Cleanup(func() { client.Close() })

	cfg := config.IssuerConfig{Server: config.ServerConfig{ListenAddress: "0.0.0.0", Port: 9090}}
	got, err := NewIssuer(context.Background(), client, cfg)
	if err != nil {
		t.Fatalf("NewIssuer() unexpected error: %v", err)
	}
	if got.Server.Addr != "0.0.0.0:9090" {
		t.Fatalf("Server.Addr = %q, want %q", got.Server.Addr, "0.0.0.0:9090")
	}
}
