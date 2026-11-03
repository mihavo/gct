package main

import (
	"context"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/mihavo/gct/internal/config"
	"github.com/mihavo/gct/internal/tailer"
)

func main() {
	ctx := context.Background()
	configPath, ok := os.LookupEnv("TAILER_CONFIG_PATH")
	if !ok {
		configPath = "./configs/tailer.yml"
	}
	cfg, err := config.LoadYAML[config.TailerConfig](configPath)
	if err != nil {
		log.Fatalf("loading tailer config: %v", err)
	}
	slog.Debug("tailer config loaded: %+v", "config", cfg)

	notifyCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	tailer, err := tailer.NewTailer(*cfg)

	if err != nil {
		log.Fatalf("Couldn't create tailer %v", err)
	}
	defer tailer.Close()

}
