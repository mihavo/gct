package main

import (
	"log"

	"github.com/mihavo/gct/internal/config"
)

func main() {
	// ctx := context.Background()

	gateConfig, err := config.LoadYAML[config.GateConfig]("./configs/gate.yml")
	if err != nil {
		log.Fatalf("loading gate config: %v", err)
	}

	log.Printf("gate config loaded: %+v", gateConfig)

}
