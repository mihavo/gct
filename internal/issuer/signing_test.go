package issuer

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/mihavo/gct/internal/config"
	"github.com/mihavo/gct/internal/keystore"
	"net/http"
)

const testDelay = time.Hour

func keysAged(t *testing.T, ages ...time.Duration) []keystore.Key {
	t.Helper()
	keys := make([]keystore.Key, len(ages))
	for i, age := range ages {
		keys[i] = newTestKey(t)
		keys[i].LastModified = time.Now().Add(-age)
	}
	return keys
}

func signingKid(t *testing.T, h http.Handler) string {
	t.Helper()
	rec := postToken(h, testAPIKey)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	parsed, _, err := jwt.NewParser().ParseUnverified(body.Token, &jwt.RegisteredClaims{})
	if err != nil {
		t.Fatalf("parsing token: %v", err)
	}
	kid, _ := parsed.Header["kid"].(string)
	return kid
}

func TestTokenSigningKeyChoice(t *testing.T) {
	tests := []struct {
		name string
		ages []time.Duration
		want int
	}{
		{"single fresh key", []time.Duration{0}, 0},
		{"single old key", []time.Duration{48 * time.Hour}, 0},
		{"new key published but not yet signing", []time.Duration{72 * time.Hour, time.Minute}, 0},
		{"new key just under the delay", []time.Duration{72 * time.Hour, 59 * time.Minute}, 0},
		{"new key just over the delay", []time.Duration{72 * time.Hour, 61 * time.Minute}, 1},
		{"newest of the keys that are old enough", []time.Duration{240 * time.Hour, 48 * time.Hour, 5 * time.Minute}, 1},
		{"old key listed after the newer one", []time.Duration{48 * time.Hour, 240 * time.Hour}, 0},
		{"no key old enough uses the oldest", []time.Duration{10 * time.Minute, 20 * time.Minute}, 1},
		{"no key old enough, slice order does not matter", []time.Duration{20 * time.Minute, 10 * time.Minute}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mr := seededRedis(t)
			keys := keysAged(t, tt.ages...)
			h := NewHandler(redisClient(t, mr.Addr()), keys, testDelay).Routes()

			got := signingKid(t, h)

			if got != keys[tt.want].Kid {
				t.Fatalf("token signed with kid %q, want key %d (%q)", got, tt.want, keys[tt.want].Kid)
			}
		})
	}
}

func TestIssuerAppliesTheConfiguredDelayInMinutes(t *testing.T) {
	mr := seededRedis(t)
	keys := keysAged(t, 72*time.Hour, 30*time.Minute)
	cfg := config.IssuerConfig{
		Server:                      config.ServerConfig{ListenAddress: "localhost", Port: 9090},
		TokenExpirationDelayMinutes: 60,
	}

	issuer, err := NewIssuer(context.Background(), redisClient(t, mr.Addr()), cfg, keys)
	if err != nil {
		t.Fatalf("NewIssuer() unexpected error: %v", err)
	}

	if got := signingKid(t, issuer.Server.Handler); got != keys[0].Kid {
		t.Fatalf("token signed with kid %q, want the old key %q because the new one is 30 minutes old and the delay is 60", got, keys[0].Kid)
	}
}

func TestIssuerSwitchesToTheNewKeyOncePastTheConfiguredDelay(t *testing.T) {
	mr := seededRedis(t)
	keys := keysAged(t, 72*time.Hour, 90*time.Minute)
	cfg := config.IssuerConfig{
		Server:                      config.ServerConfig{ListenAddress: "localhost", Port: 9090},
		TokenExpirationDelayMinutes: 60,
	}

	issuer, err := NewIssuer(context.Background(), redisClient(t, mr.Addr()), cfg, keys)
	if err != nil {
		t.Fatalf("NewIssuer() unexpected error: %v", err)
	}

	if got := signingKid(t, issuer.Server.Handler); got != keys[1].Kid {
		t.Fatalf("token signed with kid %q, want the new key %q because it is 90 minutes old and the delay is 60", got, keys[1].Kid)
	}
}
