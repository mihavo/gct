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
	"github.com/mihavo/gct/internal/serve"
)

const shutdownTimeout = 8 * time.Second

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
	defer stop()

	if err := serve.Run(notifyCtx, gateInstance.Server, shutdownTimeout); err != nil {
		log.Fatalf("gate server: %v", err)
	}
	slog.Info("gate server is down")
}
