package gate

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"github.com/mihavo/gct/internal/config"
)

type Gate struct {
	config config.GateConfig
	server *http.Server
	routes *RoutingTable
}

type RoutingTable struct {
	routes []Route
}

type Route struct {
	Name     string
	Prefix   string
	Upstream *url.URL
}

func NewGate(ctx context.Context, config config.GateConfig) (*Gate, error) {

	if len(config.Routes) == 0 {
		return nil, errors.New("No routes declared")
	}
	server := http.Server{
		Addr: config.Server.ListenAddress + ":" + string(config.Server.Port),
	}
	routes, err := buildRoutingTable(config.Routes)
	if err != nil {
		return nil, err
	}

	return &Gate{
		config: config,
		server: &server,
		routes: &routes,
	}, nil
}
