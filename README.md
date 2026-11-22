# gct

gct is three Go programs in one module that work as one system. The gateway
routes and protects traffic to your services, the checker tells it which
upstreams are healthy, and the tailer shows what both are doing in one live
log stream.

## Gateway

A reverse proxy in front of your services.

- Routing by path prefix to upstream URLs from a config file
- One log line per request, with a request ID, status and duration
- Graceful shutdown that lets in-flight requests finish
- Middleware chain with panic recovery
- Per-caller rate limiting, with interchangeable algorithms
- Authentication with API keys and signed tokens, with background key refresh
- Per-upstream timeouts, retries with backoff and jitter, and a circuit breaker
- Load balancing across healthy upstream instances
- Metrics per route and status class, and trace context sent to upstreams

## Checker

An uptime poller for a list of targets.

- Checks every target on a fixed interval, each with a timeout
- A bounded worker pool, so one hanging target can't delay the others
- Prints a results table, and gives the gateway each target's health status

## Tailer

A live view of several log sources at once.

- Follows several files as they grow and merges them into one stream
- Prefixes each line with its source
- Survives log rotation and truncation

## Running

Run the gateway demo with `docker compose up --build` in `examples/compose`,
and run the tests with `go test -race ./...`.

### API keys

Every request to the gateway needs an `X-API-Key` header. Create a key for a
client with the `keygen` service, which prints the key once and stores only its
hash in Redis:

```bash
docker compose run --rm keygen billing-service
```

Then send it with the request. The upstream receives the client's name in
`X-Client-Id` and never sees the key:

```bash
curl -H "X-API-Key: <key>" localhost:8080/auth/
```
