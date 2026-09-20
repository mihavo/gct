package config

import (
	"os"

	"github.com/goccy/go-yaml"
)

func LoadYAML[T any](path string) (*T, error) {
	data, err := os.ReadFile(path)

	if err != nil {
		return nil, err
	}
	var config T

	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	return &config, nil
}
