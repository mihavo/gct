package gate

const (
	LIMITER_REDIS_ADDR                  = "LIMITER_REDIS_ADDR"
	LIMITER_REDIS_PASSWORD              = "LIMITER_REDIS_PASSWORD"
	LIMITER_REDIS_DB                    = "LIMITER_REDIS_DB"
	LIMITER_REDIS_KEY_PREFIX            = "limiter:"
	LIMITER_REDIS_TOKENS_FIELD          = "tokens"
	LIMITER_REDIS_LAST_SUCCESSFUL_FIELD = "last_successful"
	LIMITER_MAX_RETRIES                 = 5
)
