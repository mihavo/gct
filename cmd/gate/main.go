package main

import (
	"context"
	"log"
	"log/slog"
	"os"

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

	err = gate.Server.ListenAndServe()
	if err != nil {
		log.Fatalf("could not start gate server: %v", err)
	}
}
