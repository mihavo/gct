package gate

const (
	RedisAddr     = "REDIS_ADDR"
	RedisPassword = "REDIS_PASSWORD"
	RedisDB       = "REDIS_DB"
	AuthKeyHeader = "X-API-Key"
	AuthKeyPrefix = "auth:"
	JWKSPath      = "/jwks"
)

const (
	redisKeyPrefix      = "limiter:"
	tokensField         = "tokens"
	lastSuccessfulField = "last_successful"
	maxRetries          = 5
	barWidth            = 20
)
