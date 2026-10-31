package gate

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/mihavo/gct/internal/config"
	redis "github.com/redis/go-redis/v9"
)

type Limiter struct {
	redis  *redis.Client
	config config.LimiterConfig
}

type limiterKey = string

type RefillRate struct {
	tokens int
	per    time.Duration
}

func NewLimiter(ctx context.Context, redis *redis.Client, config config.LimiterConfig) (*Limiter, error) {
	if config.BucketSize <= 0 {
		return nil, errors.New("bucket size should be greater than zero")
	}
	if config.RefillRate.Tokens <= 0 {
		return nil, errors.New("refill tokens should be greater than zero")
	}
	if config.RefillRate.Per <= 0 {
		return nil, errors.New("refill tokens interval should be greater than zero")
	}
	err := redis.Ping(ctx).Err()
	if err != nil {
		return nil, err
	}
	return &Limiter{
		redis:  redis,
		config: config,
	}, nil
}

func (l *Limiter) limit(ctx context.Context, key limiterKey, now time.Time) error {
	redisKey := LIMITER_REDIS_KEY_PREFIX + key
	retries := 0
	remaining := 0
	for {
		err := l.redis.Watch(ctx, func(tx *redis.Tx) error {
			lastSuccessful, err := tx.HGet(ctx, redisKey, LIMITER_REDIS_LAST_SUCCESSFUL_FIELD).Time()
			tokens := l.config.BucketSize

			switch {
			case errors.Is(err, redis.Nil):
				lastSuccessful = now
			case err != nil:
				return err
			default:
				tokens, err = tx.HGet(ctx, redisKey, LIMITER_REDIS_TOKENS_FIELD).Int()
				if err != nil && !errors.Is(err, redis.Nil) {
					return err
				}
			}

			intervals := int(now.Sub(lastSuccessful) / l.config.RefillRate.Per)
			if intervals > 0 {
				tokens = min(l.config.BucketSize, tokens+intervals*l.config.RefillRate.Tokens)
			}

			if tokens <= 0 {
				remaining = 0
				return ErrRateLimited
			}
			tokens--
			remaining = tokens

			_, err = tx.TxPipelined(ctx, func(p redis.Pipeliner) error {
				p.HSet(ctx, redisKey,
					LIMITER_REDIS_TOKENS_FIELD, tokens,
					LIMITER_REDIS_LAST_SUCCESSFUL_FIELD, now)
				return nil
			})
			return err
		}, redisKey)
		if errors.Is(err, redis.TxFailedErr) {
			if retries < LIMITER_MAX_RETRIES {
				retries++
				continue
			}
			return ErrRetriesExceeded
		}
		if err == nil || errors.Is(err, ErrRateLimited) {
			capacity := l.config.BucketSize
			slog.Info("bucket state",
				"ip", key,
				"bucket", bucketBar(remaining, capacity),
				"remaining", remaining,
				"consumed", capacity-remaining,
				"capacity", capacity,
				"allowed", err == nil)
		}
		return err
	}
}

func bucketBar(remaining, capacity int) string {
	width := min(capacity, LIMITER_BAR_WIDTH)
	filled := (remaining*width + capacity - 1) / capacity
	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", width-filled) + "]"
}
