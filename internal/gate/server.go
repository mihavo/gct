package gate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"

	"github.com/mihavo/gct/internal/config"
	appjson "github.com/mihavo/gct/internal/core/json"
)

type Gate struct {
	Config config.GateConfig
	Server *http.Server
}

type Handler struct {
	Routes *RoutingTable
}

type RoutingTable struct {
	routes []Route
}

type Route struct {
	Name     string
	Prefix   string
	Upstream *url.URL
	Proxy    *httputil.ReverseProxy
}

func NewGate(ctx context.Context, config config.GateConfig) (*Gate, error) {

	if len(config.Routes) == 0 {
		return nil, errors.New("No routes declared")
	}

	routes, err := buildRoutingTable(config.Routes)
	if err != nil {
		return nil, err
	}

	handler := NewHanlder(&routes)

	server := http.Server{
		Addr:    net.JoinHostPort(config.Server.ListenAddress, strconv.Itoa(config.Server.Port)),
		Handler: handler,
	}

	return &Gate{
		Config: config,
		Server: &server,
	}, nil
}

func NewHanlder(routes *RoutingTable) *Handler {
	return &Handler{
		Routes: routes,
	}
}
func (h *Handler) Lookup(path string) (Route, bool) {

	for _, route := range h.Routes.routes {
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

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	route, ok := h.Lookup(r.URL.Path)

	if !ok {
		appjson.WriteError(w, fmt.Sprintf(ErrPathNotFound.Error(), r.URL.Path), http.StatusNotFound)
		return
	}
	slog.Debug("received route: ", "route", route)
	route.Proxy.ServeHTTP(w, r)
}
