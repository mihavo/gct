package main

import (
	"context"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/mihavo/gct/internal/config"
	"github.com/mihavo/gct/internal/gate"
	"github.com/redis/go-redis/v9"
)

func main() {
	ctx := context.Background()

	configPath, ok := os.LookupEnv("GATE_CONFIG_PATH")
	if !ok {
		configPath = "./configs/gate.yml"
	}
	gateConfig, err := config.LoadYAML[config.GateConfig](configPath)
	if err != nil {
		log.Fatalf("loading gate config: %v", err)
	}
	slog.Debug("gate config loaded: %+v", "config", gateConfig)

	redis := buildRedisInstance()
	defer redis.Close()

	startupCtx, cancelStartup := context.WithTimeout(ctx, 5*time.Second)
	gate, err := gate.NewGate(startupCtx, redis, *gateConfig)
	cancelStartup()
	if err != nil {
		log.Fatalf("gate init failed: %v", err)
	}
	slog.Debug("Gate: ", "gate", gate)

	notifyCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)

	errCh := make(chan error, 1)

	go func() {
		errCh <- gate.Server.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		log.Fatalf("gate server failed: %v", err)
	case <-notifyCtx.Done():
		stop()
		shutdownCtx, stop := context.WithTimeout(ctx, time.Duration(8*time.Second))
		defer stop()
		err := gate.Server.Shutdown(shutdownCtx)
		if err != nil {
			log.Fatalf("could not shutdown gate server gracefully %v", err)
			return
		}
		slog.Info("gate server is down", "context", notifyCtx.Err().Error())
	}
}

func buildRedisInstance() *redis.Client {
	opts, err := gate.RequireEnv(gate.LIMITER_REDIS_ADDR, gate.LIMITER_REDIS_PASSWORD, gate.LIMITER_REDIS_DB)

	if err != nil {
		log.Fatalf("missing opts for gate limiter %v", err)
	}

	db, err := strconv.Atoi(opts[gate.LIMITER_REDIS_DB])
	if err != nil {
		log.Fatalf("Could not parse redis db name option")
	}

	redis := redis.NewClient(&redis.Options{
		Addr:     opts[gate.LIMITER_REDIS_ADDR],
		Password: opts[gate.LIMITER_REDIS_PASSWORD],
		DB:       db})
	return redis
}
