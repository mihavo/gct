package checker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mihavo/gct/internal/config"
)

func newTestChecker(t *testing.T, workers int, timeout time.Duration, targets ...Target) *Checker {
	t.Helper()
	c, err := NewChecker(config.CheckerConfig{
		Interval: 20 * time.Millisecond,
		Timeout:  timeout,
		Workers:  workers,
		Targets:  targets,
	})
	if err != nil {
		t.Fatalf("NewChecker: %v", err)
	}
	return c
}

func startWorkers(t *testing.T, c *Checker, workers int) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() { c.Check(ctx) })
	}
	t.Cleanup(func() {
		cancel()
		wg.Wait()
	})
}

func send(t *testing.T, c *Checker, target Target) {
	t.Helper()
	select {
	case c.targets <- target:
	case <-time.After(2 * time.Second):
		t.Fatalf("no worker took target %q", target.Name)
	}
}

func receive(t *testing.T, c *Checker, within time.Duration) Result {
	t.Helper()
	select {
	case r := <-c.results:
		return r
	case <-time.After(within):
		t.Fatalf("no result within %s", within)
		return Result{}
	}
}

func blockingServer(t *testing.T) *httptest.Server {
	t.Helper()
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })
	return srv
}

func TestNewCheckerValidation(t *testing.T) {
	valid := config.CheckerConfig{
		Interval: time.Second,
		Timeout:  time.Second,
		Workers:  2,
		Targets: []Target{
			{Name: "a", URL: "http://localhost/a", ExpectedStatus: 200},
			{Name: "b", URL: "http://localhost/b", ExpectedStatus: 200},
		},
	}

	tests := []struct {
		name    string
		mutate  func(*config.CheckerConfig)
		wantErr string
	}{
		{"valid", func(*config.CheckerConfig) {}, ""},
		{"no targets", func(c *config.CheckerConfig) { c.Targets = nil }, "targets"},
		{"zero workers", func(c *config.CheckerConfig) { c.Workers = 0 }, "workers"},
		{"zero interval", func(c *config.CheckerConfig) { c.Interval = 0 }, "interval"},
		{"negative interval", func(c *config.CheckerConfig) { c.Interval = -time.Second }, "interval"},
		{"zero timeout", func(c *config.CheckerConfig) { c.Timeout = 0 }, "timeout"},
		{"duplicate names", func(c *config.CheckerConfig) { c.Targets[1].Name = "a" }, "duplicate"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := valid
			cfg.Targets = append([]Target(nil), valid.Targets...)
			tt.mutate(&cfg)

			c, err := NewChecker(cfg)
			if tt.wantErr == "" {
				if err != nil || c == nil {
					t.Fatalf("want a checker, got %v, %v", c, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want an error mentioning %q, got none", tt.wantErr)
			}
			if !strings.Contains(strings.ToLower(err.Error()), tt.wantErr) {
				t.Fatalf("error %q does not mention %q", err, tt.wantErr)
			}
		})
	}
}

func TestCheckClassifiesOutcomes(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	t.Cleanup(ok.Close)

	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(broken.Close)

	dropping := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			conn.Close()
		}
	}))
	t.Cleanup(dropping.Close)

	closed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	closedURL := closed.URL
	closed.Close()

	hanging := blockingServer(t)

	tests := []struct {
		name   string
		url    string
		expect int
		want   Outcome
	}{
		{"expected status", ok.URL, 200, OutcomeOK},
		{"unexpected status", broken.URL, 200, OutcomeFail},
		{"connection dropped", dropping.URL, 200, OutcomeUnknownError},
		{"nothing listening", closedURL, 200, OutcomeUnreachable},
		{"no response in time", hanging.URL, 200, OutcomeTimeout},
		{"malformed url", "http://exa mple.com", 200, OutcomeInvalid},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := Target{Name: "t", URL: tt.url, ExpectedStatus: tt.expect}
			c := newTestChecker(t, 1, 200*time.Millisecond, target)
			startWorkers(t, c, 1)

			send(t, c, target)
			r := receive(t, c, 3*time.Second)

			if r.outcome != tt.want {
				t.Fatalf("outcome = %s, want %s (error: %v)", r.outcome, tt.want, r.errorData)
			}
			if r.targetName != "t" {
				t.Fatalf("targetName = %q, want %q", r.targetName, "t")
			}
		})
	}
}

func TestInvalidTargetKeepsWorkerAlive(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	t.Cleanup(ok.Close)

	bad := Target{Name: "bad", URL: "http://exa mple.com", ExpectedStatus: 200}
	good := Target{Name: "good", URL: ok.URL, ExpectedStatus: 200}
	c := newTestChecker(t, 1, time.Second, bad, good)
	startWorkers(t, c, 1)

	for i := range 3 {
		send(t, c, bad)
		if r := receive(t, c, 2*time.Second); r.outcome != OutcomeInvalid {
			t.Fatalf("round %d: outcome = %s, want %s", i, r.outcome, OutcomeInvalid)
		}
	}

	send(t, c, good)
	if r := receive(t, c, 2*time.Second); r.outcome != OutcomeOK {
		t.Fatalf("outcome = %s, want %s", r.outcome, OutcomeOK)
	}
}

func TestHangingTargetDoesNotDelayOthers(t *testing.T) {
	hanging := blockingServer(t)
	fast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	t.Cleanup(fast.Close)

	targets := []Target{
		{Name: "hang", URL: hanging.URL, ExpectedStatus: 200},
		{Name: "a", URL: fast.URL, ExpectedStatus: 200},
		{Name: "b", URL: fast.URL, ExpectedStatus: 200},
	}
	c := newTestChecker(t, 3, 5*time.Second, targets...)
	startWorkers(t, c, 3)

	start := time.Now()
	for _, target := range targets {
		send(t, c, target)
	}

	got := map[string]Outcome{}
	for len(got) < 2 {
		r := receive(t, c, time.Second)
		got[r.targetName] = r.outcome
	}

	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("fast targets took %s while another target hung", elapsed)
	}
	for _, name := range []string{"a", "b"} {
		if got[name] != OutcomeOK {
			t.Fatalf("%s outcome = %s, want %s", name, got[name], OutcomeOK)
		}
	}
	if _, finished := got["hang"]; finished {
		t.Fatal("hanging target finished before its timeout")
	}
}

func TestScheduleKeepsCheckingWhileOneTargetHangs(t *testing.T) {
	var hangHits, fastHits atomic.Int64
	hangCounter := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hangHits.Add(1)
		<-r.Context().Done()
	}))
	t.Cleanup(hangCounter.Close)

	fast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fastHits.Add(1)
	}))
	t.Cleanup(fast.Close)

	c := newTestChecker(t, 2, 5*time.Second,
		Target{Name: "hang", URL: hangCounter.URL, ExpectedStatus: 200},
		Target{Name: "fast", URL: fast.URL, ExpectedStatus: 200},
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.Schedule(ctx)
	}()

	time.Sleep(600 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Schedule did not return after cancel")
	}

	if got := fastHits.Load(); got < 5 {
		t.Fatalf("fast target checked %d times, want at least 5", got)
	}
	if got := hangHits.Load(); got != 1 {
		t.Fatalf("hanging target requested %d times, want 1 while it is in flight", got)
	}
}

func TestScheduleStopsCleanlyMidRequest(t *testing.T) {
	hanging := blockingServer(t)
	fast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Millisecond)
	}))
	t.Cleanup(fast.Close)

	for i := range 20 {
		c := newTestChecker(t, 4, 5*time.Second,
			Target{Name: "hang", URL: hanging.URL, ExpectedStatus: 200},
			Target{Name: "a", URL: fast.URL, ExpectedStatus: 200},
			Target{Name: "b", URL: fast.URL, ExpectedStatus: 200},
		)

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			c.Schedule(ctx)
		}()

		time.Sleep(time.Duration(30+i*3) * time.Millisecond)
		cancel()

		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatalf("run %d: Schedule did not return after cancel", i)
		}
	}
}
