package gate

import "errors"

var (
	ErrPathNotFound = errors.New("Requested path %s not found")
)
