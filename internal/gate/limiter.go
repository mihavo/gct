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
	LIMITER_REDIS_ADDR                = "LIMITER_REDIS_ADDR"
	LIMITER_REDIS_PASSWORD            = "LIMITER_REDIS_PASSWORD"
	LIMITER_REDIS_DB                  = "LIMITER_REDIS_DB"
	LIMITER_REDIS_TOKENS_KEY          = "TOKENS"
	LIMITER_REDIS_LAST_SUCCESSFUL_KEY = "LAST_SUCCESSFUL"
)

type limiter struct {
	ctx    context.Context
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
		ctx:    ctx,
		redis:  client,
		config: config,
		state:  make(map[limiterKey]*limiterState)}, nil
}

func (l *limiter) limit(key limiterKey, now time.Time) error {

	return l.redis.Watch(l.ctx, func(tx *redis.Tx) error {
		lastSuccessful, err := tx.HGet(l.ctx, LIMITER_REDIS_LAST_SUCCESSFUL_KEY, key).Time()
		tokens := l.config.bucketSize

		switch {
		case errors.Is(err, redis.Nil):
			lastSuccessful = now
		case err != nil:
			return err
		default:
			tokens, err = tx.HGet(l.ctx, LIMITER_REDIS_TOKENS_KEY, key).Int()
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

		_, err = tx.TxPipelined(l.ctx, func(p redis.Pipeliner) error {
			p.HSet(l.ctx, LIMITER_REDIS_TOKENS_KEY, key, tokens)
			p.HSet(l.ctx, LIMITER_REDIS_LAST_SUCCESSFUL_KEY, key, now)
			return nil
		})
		return err
	}, LIMITER_REDIS_TOKENS_KEY, LIMITER_REDIS_LAST_SUCCESSFUL_KEY)
}
