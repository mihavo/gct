package gate

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"

	"github.com/mihavo/gct/internal/config"
	appjson "github.com/mihavo/gct/internal/core/json"
	"github.com/redis/go-redis/v9"
)

type Gate struct {
	Config config.GateConfig
	Server *http.Server
}

type Handler struct {
	Routes *RoutingTable
}

type ResponseWriterWithCode struct {
	code int
	http.ResponseWriter
}

type RequestID struct {
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

func NewGate(ctx context.Context, redis *redis.Client, config config.GateConfig) (*Gate, error) {

	if len(config.Routes) == 0 {
		return nil, errors.New("No routes declared")
	}

	routes, err := buildRoutingTable(config.Routes)
	if err != nil {
		return nil, err
	}

	var handler http.Handler = NewHanlder(&routes)

	limiter, err := NewLimiter(redis, config.Limiter)

	if err != nil {
		return nil, err
	}

	handler = Chain(handler,
		&LoggingMiddleware{},
		&RecoveryMiddleware{},
		&LimiterMiddleware{limiter: limiter})

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
	route.Proxy.ServeHTTP(w, r)
}

func (rw *ResponseWriterWithCode) WriteHeader(statusCode int) {
	rw.code = statusCode
	rw.ResponseWriter.WriteHeader(statusCode)
}
