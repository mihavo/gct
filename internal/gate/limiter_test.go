package gate

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var epoch = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)

func mustLimiter(t *testing.T, size, tokens int, per time.Duration) *limiter {
	t.Helper()
	l, err := NewLimiter(limiterConfig{bucketSize: size, refillRate: refillRate{tokens: tokens, per: per}})
	if err != nil {
		t.Fatalf("NewLimiter: %v", err)
	}
	return l
}

func TestNewLimiterValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  limiterConfig
		wantErr string
	}{
		{"valid", limiterConfig{bucketSize: 5, refillRate: refillRate{tokens: 2, per: time.Second}}, ""},
		{"zero bucket size", limiterConfig{bucketSize: 0, refillRate: refillRate{tokens: 2, per: time.Second}}, "bucket size"},
		{"negative bucket size", limiterConfig{bucketSize: -1, refillRate: refillRate{tokens: 2, per: time.Second}}, "bucket size"},
		{"zero refill tokens", limiterConfig{bucketSize: 5, refillRate: refillRate{tokens: 0, per: time.Second}}, "refill tokens should"},
		{"negative refill tokens", limiterConfig{bucketSize: 5, refillRate: refillRate{tokens: -2, per: time.Second}}, "refill tokens should"},
		{"zero interval", limiterConfig{bucketSize: 5, refillRate: refillRate{tokens: 2, per: 0}}, "interval"},
		{"negative interval", limiterConfig{bucketSize: 5, refillRate: refillRate{tokens: 2, per: -time.Second}}, "interval"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l, err := NewLimiter(tt.config)
			if tt.wantErr == "" {
				if err != nil || l == nil {
					t.Fatalf("want a limiter, got %v, %v", l, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want an error mentioning %q, got none", tt.wantErr)
			}
			if l != nil {
				t.Fatalf("want a nil limiter with an error, got %v", l)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error %q does not mention %q", err, tt.wantErr)
			}
		})
	}
}

type step struct {
	at   time.Duration
	key  string
	want bool
}

func repeat(count int, at time.Duration, key string, want bool) []step {
	out := make([]step, count)
	for i := range out {
		out[i] = step{at: at, key: key, want: want}
	}
	return out
}

func join(parts ...[]step) []step {
	var out []step
	for _, part := range parts {
		out = append(out, part...)
	}
	return out
}

func TestLimit(t *testing.T) {
	tests := []struct {
		name   string
		size   int
		tokens int
		per    time.Duration
		steps  []step
	}{
		{
			name: "a new key starts with a full bucket",
			size: 3, tokens: 1, per: time.Second,
			steps: join(repeat(3, 0, "a", true), repeat(1, 0, "a", false)),
		},
		{
			name: "an empty bucket stays empty until a whole interval passes",
			size: 2, tokens: 1, per: time.Second,
			steps: []step{
				{0, "a", true},
				{0, "a", true},
				{0, "a", false},
				{500 * time.Millisecond, "a", false},
				{999 * time.Millisecond, "a", false},
				{time.Second, "a", true},
				{time.Second, "a", false},
			},
		},
		{
			name: "a refill adds the tokens of every whole interval",
			size: 5, tokens: 2, per: time.Second,
			steps: join(
				repeat(5, 0, "a", true),
				repeat(1, 0, "a", false),
				repeat(4, 2500*time.Millisecond, "a", true),
				repeat(1, 2500*time.Millisecond, "a", false),
			),
		},
		{
			name: "a long silence refills only up to the bucket size",
			size: 3, tokens: 1, per: time.Second,
			steps: join(
				repeat(3, 0, "a", true),
				repeat(3, time.Hour, "a", true),
				repeat(1, time.Hour, "a", false),
			),
		},
		{
			name: "keys do not share a bucket",
			size: 1, tokens: 1, per: time.Second,
			steps: []step{
				{0, "a", true},
				{0, "a", false},
				{0, "b", true},
				{0, "b", false},
				{0, "c", true},
			},
		},
		{
			name: "time going backwards neither refills nor panics",
			size: 1, tokens: 1, per: time.Second,
			steps: []step{
				{10 * time.Second, "a", true},
				{0, "a", false},
				{5 * time.Second, "a", false},
				{11 * time.Second, "a", true},
			},
		},
		{
			name: "tokens per interval above the bucket size are capped",
			size: 2, tokens: 10, per: time.Second,
			steps: join(
				repeat(2, 0, "a", true),
				repeat(2, time.Second, "a", true),
				repeat(1, time.Second, "a", false),
			),
		},
		{
			name: "leftover time is lost, so a client under the rate is sometimes refused",
			size: 2, tokens: 2, per: time.Second,
			steps: []step{
				{0, "a", true},
				{900 * time.Millisecond, "a", true},
				{1800 * time.Millisecond, "a", false},
				{2700 * time.Millisecond, "a", true},
				{3600 * time.Millisecond, "a", true},
				{4500 * time.Millisecond, "a", false},
				{5400 * time.Millisecond, "a", true},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := mustLimiter(t, tt.size, tt.tokens, tt.per)
			for i, s := range tt.steps {
				if got := l.limit(s.key, epoch.Add(s.at)); got != s.want {
					t.Fatalf("step %d (key %q at +%s): allowed = %v, want %v", i, s.key, s.at, got, s.want)
				}
			}
		})
	}
}

func TestLimitConcurrent(t *testing.T) {
	const (
		goroutines = 32
		perRoutine = 100
		size       = 500
	)
	l := mustLimiter(t, size, 1, time.Hour)

	var allowedShared, allowedOwn atomic.Int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for g := range goroutines {
		wg.Go(func() {
			<-start
			own := fmt.Sprintf("own-%d", g)
			if l.limit(own, epoch) {
				allowedOwn.Add(1)
			}
			for range perRoutine {
				if l.limit("shared", epoch) {
					allowedShared.Add(1)
				}
			}
		})
	}
	close(start)
	wg.Wait()

	if got := allowedOwn.Load(); got != goroutines {
		t.Fatalf("first request of each own key: allowed %d of %d", got, goroutines)
	}
	if got := allowedShared.Load(); got != size {
		t.Fatalf("shared key: allowed %d of %d requests, want exactly the bucket size %d", got, goroutines*perRoutine, size)
	}
}
