package gate

import (
	"errors"
	"sync"
	"time"
)

type limiter struct {
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

func NewLimiter(config limiterConfig) (*limiter, error) {

	if config.bucketSize <= 0 {
		return nil, errors.New("bucket size should be greater than zero")
	}
	if config.refillRate.tokens <= 0 {
		return nil, errors.New("refill tokens should be greater than zero")
	}
	if config.refillRate.per <= 0 {
		return nil, errors.New("refill tokens interval should be greater than zero")
	}

	return &limiter{
		config: config,
		state:  make(map[limiterKey]*limiterState),
	}, nil
}

func (l *limiter) limit(key limiterKey, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	state := l.state[key]

	if state == nil {
		l.state[key] = &limiterState{
			tokens:         l.config.bucketSize,
			lastSuccessful: now,
		}
		state = l.state[key]
	}

	intervals := int(now.Sub(state.lastSuccessful) / l.config.refillRate.per)

	if intervals > 0 {
		state.tokens = min(l.config.bucketSize, state.tokens+intervals*l.config.refillRate.tokens)
	}

	allow := state.tokens > 0

	if allow {
		state.lastSuccessful = now
		state.tokens--
	}
	return allow
}
