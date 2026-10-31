package config

import "time"

type GateConfig struct {
	Server  ServerConfig   `yaml:"server"`
	Limiter LimiterConfig  `yaml:"limiter"`
	Routes  []RoutesConfig `yaml:"routes"`
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

type LimiterConfig struct {
	BucketSize int        `yaml:"bucket_size"`
	RefillRate RefillRate `yaml:"refill_rate"`
}

type RefillRate struct {
	Tokens int           `yaml:"tokens"`
	Per    time.Duration `yaml:"per"`
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
