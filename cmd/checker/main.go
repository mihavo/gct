package main

import (
	"context"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	_ "time/tzdata"

	"github.com/mihavo/gct/internal/checker"
	"github.com/mihavo/gct/internal/config"
)

func main() {
	ctx := context.Background()
	configPath, ok := os.LookupEnv("CHECKER_CONFIG_PATH")
	if !ok {
		configPath = "./configs/checker.yml"
	}
	checkerConfig, err := config.LoadYAML[config.CheckerConfig](configPath)
	if err != nil {
		log.Fatalf("loading gate config: %v", err)
	}
	slog.Debug("gate config loaded: %+v", "config", checkerConfig)

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	chck, err := checker.NewChecker(*checkerConfig)

	if err != nil {
		log.Fatalf("Could not instantiate checker %v", err)
	}
	chck.Schedule(ctx)

}
