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

	gate, err := gate.NewGate(ctx, *gateConfig)
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
