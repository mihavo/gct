package gate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/mihavo/gct/internal/config"
	redis "github.com/redis/go-redis/v9"
)

var epoch = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)

func limiterConfig(size, tokens int, per time.Duration) config.LimiterConfig {
	return config.LimiterConfig{
		BucketSize: size,
		RefillRate: config.RefillRate{Tokens: tokens, Per: per},
	}
}

func newClient(t *testing.T, addr string) *redis.Client {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { client.Close() })
	return client
}

func mustLimiter(t *testing.T, size, tokens int, per time.Duration) (*Limiter, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	mr.SetTime(epoch)
	l, err := NewLimiter(context.Background(), newClient(t, mr.Addr()), limiterConfig(size, tokens, per))
	if err != nil {
		t.Fatalf("NewLimiter: %v", err)
	}
	return l, mr
}

func TestNewLimiterValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  config.LimiterConfig
		wantErr string
	}{
		{"valid", limiterConfig(5, 2, time.Second), ""},
		{"zero bucket size", limiterConfig(0, 2, time.Second), "bucket size"},
		{"negative bucket size", limiterConfig(-1, 2, time.Second), "bucket size"},
		{"zero refill tokens", limiterConfig(5, 0, time.Second), "refill tokens should"},
		{"negative refill tokens", limiterConfig(5, -2, time.Second), "refill tokens should"},
		{"zero interval", limiterConfig(5, 2, 0), "interval"},
		{"negative interval", limiterConfig(5, 2, -time.Second), "interval"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mr := miniredis.RunT(t)
			l, err := NewLimiter(context.Background(), newClient(t, mr.Addr()), tt.config)
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
			name: "time going backwards neither refills nor fails",
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
			l, mr := mustLimiter(t, tt.size, tt.tokens, tt.per)
			for i, s := range tt.steps {
				mr.SetTime(epoch.Add(s.at))
				err := l.limit(context.Background(), s.key)
				if err != nil && !errors.Is(err, ErrRateLimited) {
					t.Fatalf("step %d (key %q at +%s): unexpected error %v", i, s.key, s.at, err)
				}
				if got := err == nil; got != s.want {
					t.Fatalf("step %d (key %q at +%s): allowed = %v, want %v", i, s.key, s.at, got, s.want)
				}
			}
		})
	}
}

func TestLimitStoresOneHashPerCaller(t *testing.T) {
	l, mr := mustLimiter(t, 5, 1, time.Second)
	ctx := context.Background()

	for _, key := range []string{"a", "b"} {
		if err := l.limit(ctx, key); err != nil {
			t.Fatalf("limit(%q): %v", key, err)
		}
	}
	if err := l.limit(ctx, "b"); err != nil {
		t.Fatalf("second limit(b): %v", err)
	}

	if got := mr.Keys(); len(got) != 2 {
		t.Fatalf("keys = %v, want one hash per caller", got)
	}
	for key, want := range map[string]string{"a": "4", "b": "3"} {
		redisKey := redisKeyPrefix + key
		if got := mr.HGet(redisKey, tokensField); got != want {
			t.Fatalf("%s: tokens = %q, want %q", redisKey, got, want)
		}
		if got := mr.HGet(redisKey, lastSuccessfulField); got == "" {
			t.Fatalf("%s: last successful time is missing", redisKey)
		}
	}
}

func TestLimitDeniedRequestWritesNothing(t *testing.T) {
	l, mr := mustLimiter(t, 1, 1, time.Hour)
	ctx := context.Background()
	redisKey := redisKeyPrefix + "a"

	if err := l.limit(ctx, "a"); err != nil {
		t.Fatalf("first request: %v", err)
	}
	before := mr.HGet(redisKey, lastSuccessfulField)

	mr.SetTime(epoch.Add(time.Minute))
	if err := l.limit(ctx, "a"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("second request: err = %v, want ErrRateLimited", err)
	}
	if after := mr.HGet(redisKey, lastSuccessfulField); after != before {
		t.Fatalf("last successful time changed from %q to %q on a denied request", before, after)
	}
}

func TestLimitSurvivesAnotherInstance(t *testing.T) {
	first, mr := mustLimiter(t, 3, 1, time.Hour)
	second, err := NewLimiter(context.Background(), newClient(t, mr.Addr()), limiterConfig(3, 1, time.Hour))
	if err != nil {
		t.Fatalf("second NewLimiter: %v", err)
	}

	ctx := context.Background()
	for i, l := range []*Limiter{first, second, first} {
		if err := l.limit(ctx, "shared"); err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
	}
	for i, l := range []*Limiter{second, first} {
		if err := l.limit(ctx, "shared"); !errors.Is(err, ErrRateLimited) {
			t.Fatalf("request %d after the bucket is empty: err = %v, want ErrRateLimited", i+3, err)
		}
	}
}

func TestLimitReturnsRedisFailure(t *testing.T) {
	l, mr := mustLimiter(t, 3, 1, time.Second)
	mr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := l.limit(ctx, "a")
	if err == nil {
		t.Fatal("want an error when redis is down, got none")
	}
	if errors.Is(err, ErrRateLimited) {
		t.Fatalf("a redis failure was reported as a rate limit: %v", err)
	}
}

func TestLimitConcurrent(t *testing.T) {
	const (
		goroutines = 32
		perRoutine = 100
		size       = 500
	)
	l, mr := mustLimiter(t, size, 1, time.Hour)
	ctx := context.Background()

	var allowedShared, allowedOwn, conflicts, unexpected atomic.Int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for g := range goroutines {
		wg.Go(func() {
			<-start
			own := fmt.Sprintf("own-%d", g)
			for {
				err := l.limit(ctx, own)
				if errors.Is(err, ErrRetriesExceeded) {
					continue
				}
				if err == nil {
					allowedOwn.Add(1)
				} else {
					unexpected.Add(1)
				}
				break
			}
			for range perRoutine {
				switch err := l.limit(ctx, "shared"); {
				case err == nil:
					allowedShared.Add(1)
				case errors.Is(err, ErrRateLimited):
				case errors.Is(err, ErrRetriesExceeded):
					conflicts.Add(1)
				default:
					unexpected.Add(1)
				}
			}
		})
	}
	close(start)
	wg.Wait()

	if got := unexpected.Load(); got != 0 {
		t.Fatalf("%d requests failed with an error other than a rate limit or a conflict", got)
	}
	if got := allowedOwn.Load(); got != goroutines {
		t.Fatalf("first request of each own key: allowed %d of %d", got, goroutines)
	}

	allowed := allowedShared.Load()
	if allowed > size {
		t.Fatalf("shared key: allowed %d requests, more than the bucket size %d", allowed, size)
	}
	stored, err := strconv.Atoi(mr.HGet(redisKeyPrefix+"shared", tokensField))
	if err != nil {
		t.Fatalf("stored tokens: %v", err)
	}
	if want := size - int(allowed); stored != want {
		t.Fatalf("stored tokens = %d, want %d (bucket size %d minus %d allowed); an update was lost", stored, want, size, allowed)
	}
	t.Logf("shared key: %d allowed, %d requests gave up after %d retries", allowed, conflicts.Load(), maxRetries)
}

type failingExec struct {
	failures int
	calls    int
	onFail   func(call int)
}

func (f *failingExec) DialHook(next redis.DialHook) redis.DialHook { return next }

func (f *failingExec) ProcessHook(next redis.ProcessHook) redis.ProcessHook { return next }

func (f *failingExec) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		f.calls++
		if f.calls <= f.failures {
			if f.onFail != nil {
				f.onFail(f.calls)
			}
			return redis.TxFailedErr
		}
		return next(ctx, cmds)
	}
}

func TestLimitRetries(t *testing.T) {
	redisKey := redisKeyPrefix + "a"
	maxAttempts := maxRetries + 1

	tests := []struct {
		name         string
		failures     int
		drainOnFail  int
		wantErr      error
		wantAttempts int
		wantTokens   string
	}{
		{"conflicts below the cap are retried", 2, 0, nil, 3, "0"},
		{"success on the last allowed attempt is not reported as a failure", maxRetries, 0, nil, maxAttempts, "0"},
		{"conflicts on every attempt give up", maxAttempts, 0, ErrRetriesExceeded, maxAttempts, "1"},
		{"a denial on the last allowed attempt is still a denial", maxRetries, maxRetries, ErrRateLimited, maxRetries, "0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l, mr := mustLimiter(t, 2, 1, time.Hour)
			if err := l.limit(context.Background(), "a"); err != nil {
				t.Fatalf("first request: %v", err)
			}

			hook := &failingExec{failures: tt.failures}
			if tt.drainOnFail > 0 {
				hook.onFail = func(call int) {
					if call == tt.drainOnFail {
						mr.HSet(redisKey, tokensField, "0")
					}
				}
			}
			l.redis.AddHook(hook)

			err := l.limit(context.Background(), "a")
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if got := mr.HGet(redisKey, tokensField); got != tt.wantTokens {
				t.Fatalf("stored tokens = %q, want %q", got, tt.wantTokens)
			}
			if hook.calls != tt.wantAttempts {
				t.Fatalf("transactions attempted = %d, want %d", hook.calls, tt.wantAttempts)
			}
		})
	}
}

func TestBucketBar(t *testing.T) {
	tests := []struct {
		remaining, capacity int
		want                string
	}{
		{10, 10, "[██████████]"},
		{6, 10, "[██████░░░░]"},
		{0, 10, "[░░░░░░░░░░]"},
		{1, 3, "[█░░]"},
		{500, 500, "[" + strings.Repeat("█", barWidth) + "]"},
		{1, 500, "[█" + strings.Repeat("░", barWidth-1) + "]"},
		{250, 500, "[" + strings.Repeat("█", barWidth/2) + strings.Repeat("░", barWidth/2) + "]"},
	}
	for _, tt := range tests {
		if got := bucketBar(tt.remaining, tt.capacity); got != tt.want {
			t.Errorf("bucketBar(%d, %d) = %q, want %q", tt.remaining, tt.capacity, got, tt.want)
		}
	}
}

func TestLimitLogsBucketState(t *testing.T) {
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	l, _ := mustLimiter(t, 3, 1, time.Hour)
	for range 4 {
		l.limit(context.Background(), "10.0.0.1")
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("got %d log lines, want 4 (one per request):\n%s", len(lines), buf.String())
	}
	want := []string{
		`ip=10.0.0.1 bucket=[██░] remaining=2 consumed=1 capacity=3 allowed=true`,
		`ip=10.0.0.1 bucket=[█░░] remaining=1 consumed=2 capacity=3 allowed=true`,
		`ip=10.0.0.1 bucket=[░░░] remaining=0 consumed=3 capacity=3 allowed=true`,
		`ip=10.0.0.1 bucket=[░░░] remaining=0 consumed=3 capacity=3 allowed=false`,
	}
	for i, line := range lines {
		if !strings.Contains(line, want[i]) {
			t.Errorf("line %d = %q, want it to contain %q", i, line, want[i])
		}
	}
}

func TestLimitUsesTheRedisClock(t *testing.T) {
	l, mr := mustLimiter(t, 3, 1, time.Second)
	redisKey := redisKeyPrefix + "a"

	for _, at := range []time.Time{epoch, epoch.Add(2 * time.Hour)} {
		mr.SetTime(at)
		if err := l.limit(context.Background(), "a"); err != nil {
			t.Fatalf("limit at %s: %v", at, err)
		}
		stored, err := time.Parse(time.RFC3339Nano, mr.HGet(redisKey, lastSuccessfulField))
		if err != nil {
			t.Fatalf("parsing last successful time: %v", err)
		}
		if !stored.Equal(at) {
			t.Fatalf("last successful = %s, want the redis time %s", stored, at)
		}
	}
}

func TestLimitSetsTTLToAFullRefill(t *testing.T) {
	tests := []struct {
		name    string
		size    int
		tokens  int
		per     time.Duration
		wantTTL time.Duration
	}{
		{"one token per interval", 10, 1, time.Second, 10 * time.Second},
		{"a partial last interval rounds up", 10, 3, time.Second, 4 * time.Second},
		{"tokens above the bucket size need one interval", 2, 10, time.Minute, time.Minute},
		{"sub-second interval", 3, 1, 250 * time.Millisecond, 750 * time.Millisecond},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l, mr := mustLimiter(t, tt.size, tt.tokens, tt.per)
			if err := l.limit(context.Background(), "a"); err != nil {
				t.Fatalf("limit: %v", err)
			}
			if got := mr.TTL(redisKeyPrefix + "a"); got != tt.wantTTL {
				t.Fatalf("ttl = %s, want %s", got, tt.wantTTL)
			}
		})
	}
}

func TestLimitRenewsTTLOnlyOnAllowedRequests(t *testing.T) {
	ctx := context.Background()
	redisKey := redisKeyPrefix + "a"

	t.Run("an allowed request restarts the countdown", func(t *testing.T) {
		l, mr := mustLimiter(t, 5, 1, time.Second)
		if err := l.limit(ctx, "a"); err != nil {
			t.Fatalf("first request: %v", err)
		}
		mr.FastForward(3 * time.Second)
		if got := mr.TTL(redisKey); got != 2*time.Second {
			t.Fatalf("ttl after 3s = %s, want 2s", got)
		}
		if err := l.limit(ctx, "a"); err != nil {
			t.Fatalf("second request: %v", err)
		}
		if got := mr.TTL(redisKey); got != 5*time.Second {
			t.Fatalf("ttl after an allowed request = %s, want 5s", got)
		}
	})

	t.Run("a denied request leaves the countdown alone", func(t *testing.T) {
		l, mr := mustLimiter(t, 1, 1, time.Hour)
		if err := l.limit(ctx, "a"); err != nil {
			t.Fatalf("first request: %v", err)
		}
		mr.FastForward(10 * time.Minute)
		if err := l.limit(ctx, "a"); !errors.Is(err, ErrRateLimited) {
			t.Fatalf("second request: err = %v, want ErrRateLimited", err)
		}
		if got := mr.TTL(redisKey); got != 50*time.Minute {
			t.Fatalf("ttl after a denied request = %s, want 50m", got)
		}
	})
}

func TestLimitExpiredKeyActsLikeAFullBucket(t *testing.T) {
	l, mr := mustLimiter(t, 2, 1, time.Second)
	ctx := context.Background()
	redisKey := redisKeyPrefix + "a"

	for i := range 2 {
		if err := l.limit(ctx, "a"); err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
	}
	if err := l.limit(ctx, "a"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("empty bucket: err = %v, want ErrRateLimited", err)
	}

	mr.FastForward(2 * time.Second)
	if mr.Exists(redisKey) {
		t.Fatal("key still exists after a full refill time")
	}
	if err := l.limit(ctx, "a"); err != nil {
		t.Fatalf("request after expiry: %v", err)
	}
	if got := mr.HGet(redisKey, tokensField); got != "1" {
		t.Fatalf("tokens after expiry and one request = %q, want 1", got)
	}
}
