package issuer

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"

	"github.com/mihavo/gct/internal/config"
	"github.com/redis/go-redis/v9"
)

type Issuer struct {
	Config config.IssuerConfig
	redis  *redis.Client
	Server *http.Server
}

func NewIssuer(ctx context.Context, redis *redis.Client, config config.IssuerConfig) (*Issuer, error) {

	if redis == nil {
		return nil, errors.New("no redis instance provided")
	}

	if config.Server.ListenAddress == "" {
		return nil, errors.New("no listen address provided")
	}

	if config.Server.Port < 1 || config.Server.Port > 65535 {
		return nil, fmt.Errorf("invalid port %d: must be between 1 and 65535", config.Server.Port)
	}

	server := &http.Server{
		Addr:    net.JoinHostPort(config.Server.ListenAddress, strconv.Itoa(config.Server.Port)),
		Handler: NewHandler(redis).Routes(),
	}

	return &Issuer{
		Config: config,
		redis:  redis,
		Server: server,
	}, nil
}
