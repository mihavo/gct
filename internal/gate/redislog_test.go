package gate

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestRedisLoggerWritesThroughSlog(t *testing.T) {
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	redisLogger{}.Printf(context.Background(), "redis: failed to dial %s after %d attempts", "cache:6379", 5)

	got := buf.String()
	if !strings.Contains(got, "level=WARN") {
		t.Errorf("record is not at warn level: %q", got)
	}
	if !strings.Contains(got, "redis: failed to dial cache:6379 after 5 attempts") {
		t.Errorf("record does not carry the formatted message: %q", got)
	}
}
