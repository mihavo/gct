package gate

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/mihavo/gct/internal/config"
)

type Gate struct {
	Config config.GateConfig
	Server *http.Server
	Routes *RoutingTable
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
		Addr: net.JoinHostPort(config.Server.ListenAddress, strconv.Itoa(config.Server.Port)),
	}
	routes, err := buildRoutingTable(config.Routes)
	if err != nil {
		return nil, err
	}

	return &Gate{
		Config: config,
		Server: &server,
		Routes: &routes,
	}, nil
}

func (g *Gate) Lookup(path string) (Route, bool) {
	for _, route := range g.Routes.routes {
		prefix := route.Prefix
		if !strings.HasSuffix(prefix, "/") {
			prefix += "/"
		}
		if strings.HasPrefix(path, prefix) || path == route.Prefix {
			return route, true
		}
	}
	return Route{}, false
}
