package gate

import (
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

func NewRedisClient() (*redis.Client, error) {
	opts, err := RequireEnv(RedisAddr, RedisPassword, RedisDB)
	if err != nil {
		return nil, fmt.Errorf("missing redis options: %w", err)
	}

	db, err := strconv.Atoi(opts[RedisDB])
	if err != nil {
		return nil, fmt.Errorf("could not parse redis db option: %w", err)
	}

	return redis.NewClient(&redis.Options{
		Addr:        opts[RedisAddr],
		Password:    opts[RedisPassword],
		DB:          db,
		ReadTimeout: time.Millisecond * 200,
		DialTimeout: time.Millisecond * 1000,
		MaxRetries:  -1}), nil
}
