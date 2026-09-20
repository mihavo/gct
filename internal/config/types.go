package config

import "time"

type GateConfig struct {
	Server ServerConfig   `yaml:"server"`
	Routes []RoutesConfig `yaml:"routes"`
}

type ServerConfig struct {
	ListenAddress string `yaml:"listen_address"`
	Port          int    `yaml:"port"`
	EnableSsl     bool   `yaml:"enable_ssl"`
}

type RoutesConfig struct {
	Name     string `yaml:"name"`
	Prefix   string `yaml:"prefix"`
	Location string `yaml:"location"`
}

type CheckerConfig struct {
	Interval time.Duration  `yaml:"interval"`
	Timeout  time.Duration  `yaml:"timeout"`
	Workers  int            `yaml:"workers"`
	Targets  []TargetConfig `yaml:"targets"`
}

type TargetConfig struct {
	Name           string `yaml:"name"`
	URL            string `yaml:"url"`
	ExpectedStatus int    `yaml:"expected_status"`
}

type TailerConfig struct {
	Sources []SourceConfig `yaml:"sources"`
}

type SourceConfig struct {
	Name string `yaml:"name"`
	Path string `yaml:"path"`
}
