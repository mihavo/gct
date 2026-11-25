package issuer

import "time"

const (
	TokenExpirationInterval = time.Minute * 10
	KeyRemovalErrorMargin   = time.Minute * 5
)
