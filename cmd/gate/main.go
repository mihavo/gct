package main

import (
	"context"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mihavo/gct/internal/config"
	"github.com/mihavo/gct/internal/gate"
	"github.com/mihavo/gct/internal/logging"
)

func main() {
	ctx := context.Background()

	_, closeLogs, err := logging.Setup()
	if err != nil {
		log.Fatalf("setting up logging: %v", err)
	}
	defer closeLogs()
	gate.RouteRedisLogsToSlog()

	configPath, ok := os.LookupEnv("GATE_CONFIG_PATH")
	if !ok {
		configPath = "./configs/gate.yml"
	}
	gateConfig, err := config.LoadYAML[config.GateConfig](configPath)
	if err != nil {
		log.Fatalf("loading gate config: %v", err)
	}
	slog.Debug("gate config loaded: %+v", "config", gateConfig)

	redisClient, err := gate.NewRedisClient()
	if err != nil {
		log.Fatalf("creating redis client: %v", err)
	}
	defer redisClient.Close()

	startupCtx, cancelStartup := context.WithTimeout(ctx, 5*time.Second)
	gateInstance, err := gate.NewGate(startupCtx, redisClient, *gateConfig)
	cancelStartup()
	if err != nil {
		log.Fatalf("gate init failed: %v", err)
	}
	slog.Debug("Gate: ", "gate", gateInstance)

	notifyCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)

	errCh := make(chan error, 1)

	go func() {
		errCh <- gateInstance.Server.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		log.Fatalf("gate server failed: %v", err)
	case <-notifyCtx.Done():
		stop()
		shutdownCtx, stop := context.WithTimeout(ctx, time.Duration(8*time.Second))
		defer stop()
		err := gateInstance.Server.Shutdown(shutdownCtx)
		if err != nil {
			log.Fatalf("could not shutdown gate server gracefully %v", err)
			return
		}
		slog.Info("gate server is down", "context", notifyCtx.Err().Error())
	}
}
