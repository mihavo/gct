package issuer

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/mihavo/gct/internal/config"
	"github.com/mihavo/gct/internal/keystore"
	"github.com/redis/go-redis/v9"
)

type Issuer struct {
	Config config.IssuerConfig
	redis  *redis.Client
	Server *http.Server
	keys   []keystore.Key
}

func NewIssuer(ctx context.Context, redis *redis.Client, config config.IssuerConfig, keys []keystore.Key) (*Issuer, error) {

	if redis == nil {
		return nil, errors.New("no redis instance provided")
	}

	if config.Server.ListenAddress == "" {
		return nil, errors.New("no listen address provided")
	}

	if config.Server.Port < 1 || config.Server.Port > 65535 {
		return nil, fmt.Errorf("invalid port %d: must be between 1 and 65535", config.Server.Port)
	}

	if config.TokenExpirationDelayMinutes < 1 {
		return nil, fmt.Errorf("invalid token expiration delay %d: must be at least 1 minute", config.TokenExpirationDelayMinutes)
	}

	if len(keys) == 0 {
		return nil, errors.New("no keys present for issuer")
	}

	server := &http.Server{
		Addr:    net.JoinHostPort(config.Server.ListenAddress, strconv.Itoa(config.Server.Port)),
		Handler: NewHandler(redis, keys, time.Duration(config.TokenExpirationDelayMinutes)*time.Minute).Routes(),
	}

	return &Issuer{
		Config: config,
		redis:  redis,
		Server: server,
		keys:   keys,
	}, nil
}
