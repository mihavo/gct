package tailer

import (
	"bufio"
	"errors"
	"log/slog"
	"os"

	"github.com/mihavo/gct/internal/config"
)

type Tailer struct {
	config  config.TailerConfig
	readers []*bufio.Reader
	files   []*os.File
}

func NewTailer(config config.TailerConfig) (*Tailer, error) {
	readers := make([]*bufio.Reader, len(config.Sources))
	files := make([]*os.File, len(config.Sources))

	if len(config.Sources) == 0 {
		return nil, errors.New("no sources provided in config")
	}

	for i, source := range config.Sources {
		file, err := os.Open(source.Path)
		if err != nil {
			if closeErr := closeFiles(files); closeErr != nil {
				slog.Error("Couldn't close files", "error", closeErr)
			}
			if errors.Is(err, os.ErrNotExist) {
				return nil, errors.New("Source " + source.Name + " has invalid path")
			}
			return nil, errors.New("Couldn't open source" + source.Name)
		}
		readers[i] = bufio.NewReader(file)
		files[i] = file
	}

	return &Tailer{
		config:  config,
		readers: readers,
		files:   files,
	}, nil
}

func (t *Tailer) Close() {
	closeFiles(t.files)
}

func closeFiles(files []*os.File) error {
	errs := make([]error, 0)
	for _, file := range files {
		if file != nil {
			errs = append(errs, file.Close())
		}
	}
	return errors.Join(errs...)
}
