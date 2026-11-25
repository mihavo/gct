package issuer

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"

	"github.com/mihavo/gct/internal/config"
	"github.com/mihavo/gct/internal/keystore"
	"github.com/redis/go-redis/v9"
)

func testKeys(t *testing.T) []keystore.Key {
	t.Helper()
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}
	kid, err := keystore.KeyID(&private.PublicKey)
	if err != nil {
		t.Fatalf("computing kid: %v", err)
	}
	return []keystore.Key{{Kid: kid, Key: private}}
}

func TestNewIssuerConfig(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "localhost:0"})
	t.Cleanup(func() { client.Close() })

	keys := testKeys(t)
	valid := config.IssuerConfig{Server: config.ServerConfig{ListenAddress: "localhost", Port: 9090}, TokenExpirationDelayMinutes: 60}

	tests := []struct {
		name    string
		client  *redis.Client
		cfg     config.IssuerConfig
		keys    []keystore.Key
		wantErr bool
	}{
		{"valid", client, valid, keys, false},
		{"nil redis", nil, valid, keys, true},
		{"nil keys", client, valid, nil, true},
		{"empty keys", client, valid, []keystore.Key{}, true},
		{"empty listen address", client, config.IssuerConfig{Server: config.ServerConfig{Port: 9090}, TokenExpirationDelayMinutes: 60}, keys, true},
		{"zero value config", client, config.IssuerConfig{}, keys, true},
		{"no delay", client, config.IssuerConfig{Server: config.ServerConfig{ListenAddress: "localhost", Port: 9090}}, keys, true},
		{"negative delay", client, config.IssuerConfig{Server: config.ServerConfig{ListenAddress: "localhost", Port: 9090}, TokenExpirationDelayMinutes: -5}, keys, true},
		{"smallest delay", client, config.IssuerConfig{Server: config.ServerConfig{ListenAddress: "localhost", Port: 9090}, TokenExpirationDelayMinutes: 1}, keys, false},
		{"port zero", client, config.IssuerConfig{Server: config.ServerConfig{ListenAddress: "localhost"}, TokenExpirationDelayMinutes: 60}, keys, true},
		{"negative port", client, config.IssuerConfig{Server: config.ServerConfig{ListenAddress: "localhost", Port: -1}, TokenExpirationDelayMinutes: 60}, keys, true},
		{"port above range", client, config.IssuerConfig{Server: config.ServerConfig{ListenAddress: "localhost", Port: 65536}, TokenExpirationDelayMinutes: 60}, keys, true},
		{"lowest port", client, config.IssuerConfig{Server: config.ServerConfig{ListenAddress: "localhost", Port: 1}, TokenExpirationDelayMinutes: 60}, keys, false},
		{"highest port", client, config.IssuerConfig{Server: config.ServerConfig{ListenAddress: "localhost", Port: 65535}, TokenExpirationDelayMinutes: 60}, keys, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewIssuer(context.Background(), tt.client, tt.cfg, tt.keys)
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

func TestNewIssuerKeepsKeys(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "localhost:0"})
	t.Cleanup(func() { client.Close() })

	keys := testKeys(t)
	cfg := config.IssuerConfig{Server: config.ServerConfig{ListenAddress: "localhost", Port: 9090}, TokenExpirationDelayMinutes: 60}

	got, err := NewIssuer(context.Background(), client, cfg, keys)
	if err != nil {
		t.Fatalf("NewIssuer() unexpected error: %v", err)
	}
	if len(got.keys) != 1 || got.keys[0].Kid != keys[0].Kid {
		t.Fatalf("issuer keys = %+v, want the key with kid %q", got.keys, keys[0].Kid)
	}
}

func TestNewIssuerAddr(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "localhost:0"})
	t.Cleanup(func() { client.Close() })

	cfg := config.IssuerConfig{Server: config.ServerConfig{ListenAddress: "0.0.0.0", Port: 9090}, TokenExpirationDelayMinutes: 60}
	got, err := NewIssuer(context.Background(), client, cfg, testKeys(t))
	if err != nil {
		t.Fatalf("NewIssuer() unexpected error: %v", err)
	}
	if got.Server.Addr != "0.0.0.0:9090" {
		t.Fatalf("Server.Addr = %q, want %q", got.Server.Addr, "0.0.0.0:9090")
	}
}
