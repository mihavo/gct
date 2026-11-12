package logging

import (
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func restoreLogOutput(t *testing.T) {
	t.Helper()
	previous := log.Writer()
	t.Cleanup(func() { log.SetOutput(previous) })
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}

func TestSetupWithoutLogFileUsesStdout(t *testing.T) {
	restoreLogOutput(t)
	t.Setenv(EnvLogFile, "")
	before := log.Writer()

	out, cleanup, err := Setup()
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	defer cleanup()

	if out != os.Stdout {
		t.Errorf("writer = %v, want os.Stdout", out)
	}
	if log.Writer() != before {
		t.Error("log output was changed although LOG_FILE is unset")
	}
}

func TestSetupSendsLogAndSlogOutputToTheFile(t *testing.T) {
	restoreLogOutput(t)
	path := filepath.Join(t.TempDir(), "app.log")
	t.Setenv(EnvLogFile, path)

	out, cleanup, err := Setup()
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}

	log.Print("from log")
	slog.Info("from slog", "key", "value")
	fmt.Fprintln(out, "from writer")
	cleanup()

	got := readFile(t, path)
	for _, want := range []string{"from log", "from slog", "key=value", "from writer"} {
		if !strings.Contains(got, want) {
			t.Errorf("log file is missing %q, got:\n%s", want, got)
		}
	}
}

func TestSetupAppendsToAnExistingFile(t *testing.T) {
	restoreLogOutput(t)
	path := filepath.Join(t.TempDir(), "app.log")
	if err := os.WriteFile(path, []byte("earlier line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvLogFile, path)

	out, cleanup, err := Setup()
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	fmt.Fprintln(out, "later line")
	cleanup()

	if got := readFile(t, path); got != "earlier line\nlater line\n" {
		t.Errorf("log file = %q, want both lines in order", got)
	}
}

func TestSetupCleanupRestoresLogOutput(t *testing.T) {
	restoreLogOutput(t)
	t.Setenv(EnvLogFile, filepath.Join(t.TempDir(), "app.log"))

	_, cleanup, err := Setup()
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	cleanup()

	if log.Writer() != os.Stderr {
		t.Errorf("log output = %v after cleanup, want os.Stderr", log.Writer())
	}
}

func TestSetupFailsWhenTheFileCannotBeOpened(t *testing.T) {
	restoreLogOutput(t)
	path := filepath.Join(t.TempDir(), "missing-dir", "app.log")
	t.Setenv(EnvLogFile, path)
	before := log.Writer()

	out, cleanup, err := Setup()
	if err == nil {
		cleanup()
		t.Fatal("Setup succeeded for a path inside a missing directory")
	}
	if out != nil || cleanup != nil {
		t.Error("Setup returned a writer or cleanup together with an error")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q does not name the path", err)
	}
	if log.Writer() != before {
		t.Error("log output was changed although opening the file failed")
	}
}
