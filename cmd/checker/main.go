package main

import (
	"log"

	"github.com/mihavo/gct/internal/config"
)

func main() {
	checkerConfig, err := config.LoadYAML[config.CheckerConfig]("./configs/checker.yml")
	if err != nil {
		log.Fatalf("loading checker config: %v", err)
	}

	log.Printf("checker config loaded: %+v", checkerConfig)
}
