package checker

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/mihavo/gct/internal/config"
)

type Checker struct {
	Config  config.CheckerConfig
	client  *http.Client
	targets chan Target
	results chan Result
	busy    chan string
}

type Target = config.TargetConfig

type Result struct {
	statusCode     int
	duration       time.Duration
	targetName     string
	errorData      error
	expectedStatus int
	outcome        Outcome
}

type Outcome int

const (
	OutcomeUnknownError Outcome = iota
	OutcomeOK
	OutcomeFail
	OutcomeTimeout
	OutcomeUnreachable
	OutcomeInvalid
	OutcomePending
)

func NewChecker(config config.CheckerConfig) (*Checker, error) {

	if len(config.Targets) == 0 {
		return nil, errors.New("No targets defined")
	}

	if config.Workers <= 0 {
		return nil, errors.New("No workers amount defined")
	}

	if config.Interval <= 0 {
		return nil, errors.New("interval must be positive")
	}

	if config.Timeout <= 0 {
		return nil, errors.New("timeout must be positive")
	}

	names := make(map[string]struct{}, len(config.Targets))
	for _, target := range config.Targets {
		if _, ok := names[target.Name]; ok {
			return nil, fmt.Errorf("duplicate target name %q", target.Name)
		}
		names[target.Name] = struct{}{}
	}

	return &Checker{
		Config:  config,
		client:  &http.Client{},
		targets: make(chan Target),
		results: make(chan Result),
		busy:    make(chan string),
	}, nil
}
