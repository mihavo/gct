package tailer

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

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
		_, err = file.Seek(0, io.SeekEnd)

		if err != nil {
			return nil, errors.New("Failed to set offset for file" + file.Name())
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

func (t *Tailer) Watch(ctx context.Context, idx int) {
	line, err := t.readers[idx].ReadString('\n')
	if errors.Is(err, io.EOF) {
		return
	}
	fmt.Printf("%s: %s", t.config.Sources[idx].Name, strings.Trim(line, "\n"))
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

func (t *Tailer) StreamAll(ctx context.Context) {
	var tailers sync.WaitGroup
	for i := 0; i < len(t.config.Sources); i++ {
		tailers.Go(func() {
			t.Stream(ctx, i)
		})
	}
	defer tailers.Wait()
}

func (t *Tailer) Stream(ctx context.Context, idx int) {
	pending := ""
	var timer *time.Timer
	for {
		line, err := t.readers[idx].ReadString('\n')
		switch {
		case err == nil:
			fmt.Printf("[%s] %s%s", t.config.Sources[idx].Name, pending, line)
			pending = ""
			continue
		case len(line) > 0 && err == io.EOF:
			pending += line
			timer = time.NewTimer(time.Duration(200 * time.Millisecond))
		case len(line) == 0 && err == io.EOF:
			timer = time.NewTimer(time.Duration(200 * time.Millisecond))
		case err != nil:
			slog.Error("Tailer error", "error", err)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
	}
}
