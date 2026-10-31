package gate

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"time"

	redis "github.com/redis/go-redis/v9"
)

const (
	LIMITER_REDIS_ADDR                  = "LIMITER_REDIS_ADDR"
	LIMITER_REDIS_PASSWORD              = "LIMITER_REDIS_PASSWORD"
	LIMITER_REDIS_DB                    = "LIMITER_REDIS_DB"
	LIMITER_REDIS_KEY_PREFIX            = "limiter:"
	LIMITER_REDIS_TOKENS_FIELD          = "tokens"
	LIMITER_REDIS_LAST_SUCCESSFUL_FIELD = "last_successful"
	LIMITER_MAX_RETRIES                 = 5
)

type limiter struct {
	redis  *redis.Client
	config limiterConfig
	state  map[limiterKey]*limiterState
	mu     sync.Mutex
}

type limiterKey = string

type limiterConfig struct {
	bucketSize int
	refillRate refillRate
}

type limiterState struct {
	tokens         int
	lastSuccessful time.Time
}

type refillRate struct {
	tokens int
	per    time.Duration
}

func NewLimiter(ctx context.Context, config limiterConfig) (*limiter, error) {
	if config.bucketSize <= 0 {
		return nil, errors.New("bucket size should be greater than zero")
	}
	if config.refillRate.tokens <= 0 {
		return nil, errors.New("refill tokens should be greater than zero")
	}
	if config.refillRate.per <= 0 {
		return nil, errors.New("refill tokens interval should be greater than zero")
	}

	opts, err := requireEnv(LIMITER_REDIS_ADDR, LIMITER_REDIS_PASSWORD, LIMITER_REDIS_DB)

	if err != nil {
		return nil, err
	}
	db, err := strconv.Atoi(opts[LIMITER_REDIS_DB])

	if err != nil {
		return nil, errors.New("Could not parse redis db name option")
	}
	client := redis.NewClient(&redis.Options{
		Addr:     opts[LIMITER_REDIS_ADDR],
		Password: opts[LIMITER_REDIS_PASSWORD],
		DB:       db})

	return &limiter{
		redis:  client,
		config: config,
		state:  make(map[limiterKey]*limiterState)}, nil
}

func (l *limiter) limit(ctx context.Context, key limiterKey, now time.Time) error {
	redisKey := LIMITER_REDIS_KEY_PREFIX + key
	retries := 0
	for {
		err := l.redis.Watch(ctx, func(tx *redis.Tx) error {
			lastSuccessful, err := tx.HGet(ctx, redisKey, LIMITER_REDIS_LAST_SUCCESSFUL_FIELD).Time()
			tokens := l.config.bucketSize

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

			intervals := int(now.Sub(lastSuccessful) / l.config.refillRate.per)
			if intervals > 0 {
				tokens = min(l.config.bucketSize, tokens+intervals*l.config.refillRate.tokens)
			}

			if tokens <= 0 {
				return ErrRateLimited
			}
			tokens--

			_, err = tx.TxPipelined(ctx, func(p redis.Pipeliner) error {
				p.HSet(ctx, redisKey,
					LIMITER_REDIS_TOKENS_FIELD, tokens,
					LIMITER_REDIS_LAST_SUCCESSFUL_FIELD, now)
				return nil
			})
			return err
		}, redisKey)
		if errors.Is(err, redis.TxFailedErr) && retries < LIMITER_MAX_RETRIES {
			retries++
			continue
		}
		return err
	}
}
