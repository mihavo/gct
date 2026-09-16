package config

import "github.com/mihavo/gct/internal/core"

type Config struct {
	Data   map[string]Config
	Domain core.Domain
}

func LoadYAML(path string) {

}
