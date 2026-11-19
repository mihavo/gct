package tailer

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/mihavo/gct/internal/config"
)

type Tailer struct {
	config  config.TailerConfig
	readers []*bufio.Reader
	files   []*os.File
	lines   chan string
	Out     io.Writer
	outFile *os.File
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
			closeAndLog(files)
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

	var out io.Writer = os.Stdout
	var outFile *os.File
	if config.Out != "" {
		file, err := os.OpenFile(config.Out, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			closeAndLog(files)
			return nil, fmt.Errorf("couldn't open output %s: %w", config.Out, err)
		}
		out = file
		outFile = file
	}

	return &Tailer{
		config:  config,
		readers: readers,
		files:   files,
		lines:   make(chan string),
		Out:     out,
		outFile: outFile,
	}, nil
}

func (t *Tailer) Close() error {
	return closeFiles(append(t.files[:len(t.files):len(t.files)], t.outFile)...)
}

func closeAndLog(files []*os.File) {
	if err := closeFiles(files...); err != nil {
		slog.Error("Couldn't close files", "error", err)
	}
}

func closeFiles(files ...*os.File) error {
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
	var printer sync.WaitGroup
	for i := 0; i < len(t.config.Sources); i++ {
		tailers.Go(func() {
			t.Stream(ctx, i)
		})
	}
	printer.Go(func() {
		t.Print()
	})
	tailers.Wait()
	close(t.lines)
	printer.Wait()
}

func (t *Tailer) Stream(ctx context.Context, idx int) {
	pending := ""
	var timer *time.Timer
	for {
		line, err := t.readers[idx].ReadString('\n')
		switch {
		case err == nil:
			select {
			case <-ctx.Done():
				return
			case t.lines <- fmt.Sprintf("[%s] %s%s", t.config.Sources[idx].Name, pending, line):
			}
			pending = ""
			continue
		case len(line) > 0 && err == io.EOF:
			if rerr := t.reopenIfRotated(idx); rerr != nil {
				slog.Error("Rotation check failed", "source", t.config.Sources[idx].Name, "error", rerr)
			}
			pending += line
			timer = time.NewTimer(time.Duration(200 * time.Millisecond))
		case len(line) == 0 && err == io.EOF:
			if rerr := t.reopenIfRotated(idx); rerr != nil {
				slog.Error("Rotation check failed", "source", t.config.Sources[idx].Name, "error", rerr)
			}
			timer = time.NewTimer(time.Duration(200 * time.Millisecond))
		default:
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

func (t *Tailer) reopenIfRotated(idx int) error {
	path := t.config.Sources[idx].Path

	current, err := t.files[idx].Stat()

	if err != nil {
		return fmt.Errorf("stat open file: %w", err)
	}
	onDisk, err := os.Stat(path)

	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}

	if os.SameFile(current, onDisk) {
		return nil
	}

	next, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}

	if err := t.files[idx].Close(); err != nil {
		slog.Error("Couldn't close rotated file", "source", t.config.Sources[idx].Name, "error", err)
	}
	t.files[idx] = next
	t.readers[idx] = bufio.NewReader(next)
	return nil
}

func (t *Tailer) Print() {
	for line := range t.lines {
		fmt.Fprint(t.Out, line)
	}
}
