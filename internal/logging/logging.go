package logging

import (
	"fmt"
	"io"
	"log"
	"os"
)

const EnvLogFile = "LOG_FILE"

func Setup() (io.Writer, func(), error) {
	path := os.Getenv(EnvLogFile)
	if path == "" {
		return os.Stdout, func() {}, nil
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, nil, fmt.Errorf("opening log file %q: %w", path, err)
	}

	log.SetOutput(file)
	return file, func() {
		log.SetOutput(os.Stderr)
		_ = file.Close()
	}, nil
}
