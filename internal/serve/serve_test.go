package serve

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"
)

func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("finding a free port: %v", err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatalf("closing probe listener: %v", err)
	}
	return addr
}

func waitUp(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("server at %s did not come up", addr)
}

func runAsync(ctx context.Context, server *http.Server, timeout time.Duration) <-chan error {
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, server, timeout)
	}()
	return done
}

func waitResult(t *testing.T, done <-chan error, within time.Duration) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(within):
		t.Fatalf("Run did not return within %v", within)
		return nil
	}
}

func TestRunReturnsNilAfterCancel(t *testing.T) {
	addr := freeAddr(t)
	server := &http.Server{Addr: addr, Handler: http.NewServeMux()}

	ctx, cancel := context.WithCancel(context.Background())
	done := runAsync(ctx, server, time.Second)
	waitUp(t, addr)

	cancel()
	if err := waitResult(t, done, 2*time.Second); err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}

	if conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond); err == nil {
		conn.Close()
		t.Fatalf("server still accepts connections after Run returned")
	}
}

func TestRunReturnsListenError(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("occupying a port: %v", err)
	}
	defer busy.Close()

	server := &http.Server{Addr: busy.Addr().String(), Handler: http.NewServeMux()}
	done := runAsync(context.Background(), server, time.Second)

	err = waitResult(t, done, 2*time.Second)
	if err == nil {
		t.Fatalf("Run() = nil, want an error for a port that is already in use")
	}
}

func TestRunLetsInFlightRequestFinish(t *testing.T) {
	addr := freeAddr(t)
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		<-release
		w.WriteHeader(http.StatusOK)
	})
	server := &http.Server{Addr: addr, Handler: mux}

	ctx, cancel := context.WithCancel(context.Background())
	done := runAsync(ctx, server, 5*time.Second)
	waitUp(t, addr)

	status := make(chan int, 1)
	go func() {
		resp, err := http.Get("http://" + addr + "/slow")
		if err != nil {
			status <- -1
			return
		}
		resp.Body.Close()
		status <- resp.StatusCode
	}()
	<-started

	cancel()
	select {
	case err := <-done:
		t.Fatalf("Run returned %v while a request was still in flight", err)
	case <-time.After(150 * time.Millisecond):
	}

	close(release)
	if got := <-status; got != http.StatusOK {
		t.Fatalf("in-flight request status = %d, want %d", got, http.StatusOK)
	}
	if err := waitResult(t, done, 2*time.Second); err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}
}

func TestRunReportsShutdownTimeout(t *testing.T) {
	addr := freeAddr(t)
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/stuck", func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		<-release
	})
	server := &http.Server{Addr: addr, Handler: mux}
	t.Cleanup(func() {
		close(release)
		server.Close()
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := runAsync(ctx, server, 100*time.Millisecond)
	waitUp(t, addr)

	go func() {
		resp, err := http.Get("http://" + addr + "/stuck")
		if err == nil {
			resp.Body.Close()
		}
	}()
	<-started

	cancel()
	err := waitResult(t, done, 2*time.Second)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run() = %v, want an error wrapping context.DeadlineExceeded", err)
	}
}
