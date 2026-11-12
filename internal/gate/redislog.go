package gate

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/redis/go-redis/v9"
)

type redisLogger struct{}

func (redisLogger) Printf(_ context.Context, format string, v ...any) {
	slog.Warn(fmt.Sprintf(format, v...))
}

func RouteRedisLogsToSlog() {
	redis.SetLogger(redisLogger{})
}
