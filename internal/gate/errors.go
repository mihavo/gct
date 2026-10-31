package gate

import "errors"

var (
	ErrPathNotFound    = errors.New("Requested path %s not found")
	ErrRateLimited     = errors.New("Too many requests, try again later")
	ErrRetriesExceeded = errors.New("Too many retries for rate limiting")
)
