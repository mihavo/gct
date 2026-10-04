package main

import (
	"context"
	"log"
	"log/slog"

	"github.com/mihavo/gct/internal/config"
	"github.com/mihavo/gct/internal/gate"
)

func main() {
	ctx := context.Background()

	gateConfig, err := config.LoadYAML[config.GateConfig]("./configs/gate.yml")
	if err != nil {
		log.Fatalf("loading gate config: %v", err)
	}
	slog.Debug("gate config loaded: %+v", "config", gateConfig)

	gate, err := gate.NewGate(ctx, *gateConfig)
	if err != nil {
		log.Fatalf("gate init failed: %v", err)
	}
	slog.Debug("Gate: ", "gate", gate)
}
