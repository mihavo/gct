package checker

import (
	"errors"
	"net/http"
	"time"

	"github.com/mihavo/gct/internal/config"
)

type Checker struct {
	Config  config.CheckerConfig
	client  *http.Client
	targets chan Target
	results chan Result
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
	OutcomeOK Outcome = iota
	OutcomeFail
	OutcomeTimeout
	OutcomeUnreachable
	OutcomeInvalid
)

func NewChecker(config config.CheckerConfig) (*Checker, error) {

	if len(config.Targets) == 0 {
		return nil, errors.New("No targets defined")
	}

	if config.Workers <= 0 {
		return nil, errors.New("No workers amount defined")
	}

	return &Checker{
		Config:  config,
		client:  &http.Client{},
		targets: make(chan Target),
		results: make(chan Result),
	}, nil
}
