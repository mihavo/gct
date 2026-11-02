package gate

const (
	LimiterRedisAddr     = "LIMITER_REDIS_ADDR"
	LimiterRedisPassword = "LIMITER_REDIS_PASSWORD"
	LimiterRedisDB       = "LIMITER_REDIS_DB"
)

const (
	redisKeyPrefix      = "limiter:"
	tokensField         = "tokens"
	lastSuccessfulField = "last_successful"
	maxRetries          = 5
	barWidth            = 20
)
