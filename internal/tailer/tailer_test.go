package tailer

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mihavo/gct/internal/config"
)

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func appendToFile(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
}

func eventually(t *testing.T, what string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func newTestTailer(t *testing.T, sources ...config.SourceConfig) (*Tailer, *lockedBuffer) {
	t.Helper()
	tl, err := NewTailer(config.TailerConfig{Sources: sources})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tl.Close() })
	out := &lockedBuffer{}
	tl.Out = out
	return tl, out
}

func TestNewTailerRequiresSources(t *testing.T) {
	if _, err := NewTailer(config.TailerConfig{}); err == nil {
		t.Fatal("expected an error for a config without sources")
	}
}

func TestNewTailerNamesTheSourceThatCannotBeOpened(t *testing.T) {
	good := filepath.Join(t.TempDir(), "good.log")
	writeFile(t, good, "")

	_, err := NewTailer(config.TailerConfig{Sources: []config.SourceConfig{
		{Name: "good", Path: good},
		{Name: "missing", Path: filepath.Join(t.TempDir(), "nope.log")},
	}})
	if err == nil {
		t.Fatal("expected an error for a missing source file")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("error does not name the failing source: %v", err)
	}
}

func TestNewTailerDefaultsOutputToStdout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.log")
	writeFile(t, path, "")

	tl, err := NewTailer(config.TailerConfig{Sources: []config.SourceConfig{{Name: "a", Path: path}}})
	if err != nil {
		t.Fatal(err)
	}
	defer tl.Close()

	if tl.Out != os.Stdout {
		t.Errorf("Out = %v, want os.Stdout", tl.Out)
	}
	if tl.outFile != nil {
		t.Error("outFile should be nil when no output path is configured")
	}
}

func TestNewTailerAppendsToTheConfiguredOutputFileAndCloseReleasesIt(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.log")
	outPath := filepath.Join(dir, "out.log")
	writeFile(t, src, "")
	writeFile(t, outPath, "existing\n")

	tl, err := NewTailer(config.TailerConfig{
		Sources: []config.SourceConfig{{Name: "a", Path: src}},
		Out:     outPath,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := fmt.Fprint(tl.Out, "new\n"); err != nil {
		t.Fatalf("writing to Out: %v", err)
	}
	if err := tl.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "existing\nnew\n" {
		t.Errorf("output file = %q, want the old content followed by the new line", got)
	}
	if _, err := fmt.Fprint(tl.Out, "late\n"); err == nil {
		t.Error("writing to Out after Close should fail, so the output file was left open")
	}
}

func TestNewTailerReportsAnOutputFileItCannotOpen(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.log")
	writeFile(t, src, "")
	bad := filepath.Join(dir, "no-such-dir", "out.log")

	_, err := NewTailer(config.TailerConfig{
		Sources: []config.SourceConfig{{Name: "a", Path: src}},
		Out:     bad,
	})
	if err == nil {
		t.Fatal("expected an error for an output file in a missing directory")
	}
	if !strings.Contains(err.Error(), bad) {
		t.Errorf("error does not name the output path: %v", err)
	}
}

func TestCloseSucceedsWithoutAnOutputFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.log")
	writeFile(t, path, "")
	tl, err := NewTailer(config.TailerConfig{Sources: []config.SourceConfig{{Name: "a", Path: path}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := tl.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestStreamPrintsNewLinesWithTheSourceNameAndSkipsOldContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.log")
	writeFile(t, path, "old\n")
	tl, out := newTestTailer(t, config.SourceConfig{Name: "a", Path: path})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { tl.Stream(ctx, 0); close(done) }()
	defer func() { cancel(); <-done }()

	appendToFile(t, path, "one\ntwo\n")

	eventually(t, "both new lines", func() bool { return out.String() == "[a] one\n[a] two\n" })
}

func TestStreamHoldsBackAPartialLineUntilItIsComplete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.log")
	writeFile(t, path, "")
	tl, out := newTestTailer(t, config.SourceConfig{Name: "a", Path: path})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { tl.Stream(ctx, 0); close(done) }()
	defer func() { cancel(); <-done }()

	appendToFile(t, path, "par")
	time.Sleep(500 * time.Millisecond)
	if got := out.String(); got != "" {
		t.Fatalf("partial line was printed early: %q", got)
	}

	appendToFile(t, path, "tial\n")
	eventually(t, "the completed line", func() bool { return out.String() == "[a] partial\n" })
}

func TestStreamStopsWhenTheContextIsCancelled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.log")
	writeFile(t, path, "")
	tl, _ := newTestTailer(t, config.SourceConfig{Name: "a", Path: path})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { tl.Stream(ctx, 0); close(done) }()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stream did not return after the context was cancelled")
	}
}

func TestStreamAllReadsEverySourceAndReturnsOnCancel(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.log")
	b := filepath.Join(dir, "b.log")
	writeFile(t, a, "")
	writeFile(t, b, "")
	tl, out := newTestTailer(t,
		config.SourceConfig{Name: "a", Path: a},
		config.SourceConfig{Name: "b", Path: b},
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { tl.StreamAll(ctx); close(done) }()

	appendToFile(t, a, "from a\n")
	appendToFile(t, b, "from b\n")

	eventually(t, "lines from both sources", func() bool {
		got := out.String()
		return strings.Contains(got, "[a] from a\n") && strings.Contains(got, "[b] from b\n")
	})

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("StreamAll did not return after the context was cancelled")
	}
}

func streamInBackground(t *testing.T, tl *Tailer) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { tl.Stream(ctx, 0); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
}

func TestStreamFollowsAFileThatIsRenamedAndRecreated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.log")
	writeFile(t, path, "")
	tl, out := newTestTailer(t, config.SourceConfig{Name: "a", Path: path})
	streamInBackground(t, tl)

	appendToFile(t, path, "before\n")
	eventually(t, "the line before the rotation", func() bool { return out.String() == "[a] before\n" })

	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, "first in new file\n")

	eventually(t, "the first line of the new file", func() bool {
		return out.String() == "[a] before\n[a] first in new file\n"
	})

	appendToFile(t, path, "later\n")
	eventually(t, "a later line in the new file", func() bool {
		return strings.HasSuffix(out.String(), "[a] later\n")
	})
}

func TestStreamIgnoresTheRenamedFileAfterTheSwitch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.log")
	writeFile(t, path, "")
	tl, out := newTestTailer(t, config.SourceConfig{Name: "a", Path: path})
	streamInBackground(t, tl)

	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, "new\n")
	eventually(t, "the switch to the new file", func() bool { return out.String() == "[a] new\n" })

	appendToFile(t, path+".1", "to the old file\n")
	time.Sleep(600 * time.Millisecond)

	if got := out.String(); got != "[a] new\n" {
		t.Errorf("output = %q, want only the line from the new file", got)
	}
}

func TestStreamWaitsOutAMissingPathDuringRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.log")
	writeFile(t, path, "")
	tl, out := newTestTailer(t, config.SourceConfig{Name: "a", Path: path})
	streamInBackground(t, tl)

	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(600 * time.Millisecond)
	if got := out.String(); got != "" {
		t.Fatalf("output during the gap = %q, want nothing", got)
	}

	writeFile(t, path, "after the gap\n")
	eventually(t, "the line written after the gap", func() bool { return out.String() == "[a] after the gap\n" })
}

func TestStreamSwitchesFilesEvenWhileAPartialLineIsPending(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.log")
	writeFile(t, path, "")
	tl, out := newTestTailer(t, config.SourceConfig{Name: "a", Path: path})
	streamInBackground(t, tl)

	appendToFile(t, path, "half")
	time.Sleep(400 * time.Millisecond)

	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, "fresh\n")

	eventually(t, "the line from the new file", func() bool {
		return strings.HasSuffix(out.String(), "fresh\n")
	})
}
