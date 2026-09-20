package main

import (
	"log"

	"github.com/mihavo/gct/internal/config"
)

func main() {
	tailerConfig, err := config.LoadYAML[config.TailerConfig]("./configs/tailer.yml")
	if err != nil {
		log.Fatalf("loading tailer config: %v", err)
	}

	log.Printf("tailer config loaded: %+v", tailerConfig)
}
