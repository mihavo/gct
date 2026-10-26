package checker

import (
	"bytes"
	"cmp"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"text/tabwriter"
	"time"
)

func (o Outcome) String() string {
	switch o {
	case OutcomeOK:
		return "OK"
	case OutcomeFail:
		return "FAIL"
	case OutcomeTimeout:
		return "TIMEOUT"
	case OutcomeUnreachable:
		return "UNREACHABLE"
	case OutcomeInvalid:
		return "INVALID"
	case OutcomePending:
		return "PENDING"
	case OutcomeUnknownError:
		return "ERROR"
	default:
		return fmt.Sprintf("Outcome(%d)", int(o))
	}
}

func (o Outcome) responded() bool {
	return o == OutcomeOK || o == OutcomeFail
}

const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiDim    = "\x1b[2m"
	ansiRed    = "\x1b[31m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiMagma  = "\x1b[35m"
	ansiCyan   = "\x1b[36m"
)

func (o Outcome) color() string {
	switch o {
	case OutcomeOK:
		return ansiGreen
	case OutcomeFail:
		return ansiRed
	case OutcomeTimeout:
		return ansiYellow
	case OutcomeUnreachable:
		return ansiMagma
	case OutcomeInvalid:
		return ansiCyan
	case OutcomePending:
		return ansiDim
	case OutcomeUnknownError:
		return ansiBold + ansiRed
	default:
		return ""
	}
}

const roundTimeLayout = "2006-01-02 15:04:05"

func presentResults(w io.Writer, results []Result, now time.Time, color bool) error {
	paint := func(code, text string) string {
		if !color || code == "" {
			return text
		}
		return code + text + ansiReset
	}

	stamp := fmt.Sprintf("round at %s", now.Format(roundTimeLayout))
	if _, err := fmt.Fprintln(w, paint(ansiDim, stamp)); err != nil {
		return err
	}

	if len(results) == 0 {
		_, err := fmt.Fprintln(w, "no results")
		return err
	}

	rows := slices.Clone(results)
	slices.SortFunc(rows, func(a, b Result) int {
		return cmp.Compare(a.targetName, b.targetName)
	})

	var table bytes.Buffer
	tw := tabwriter.NewWriter(&table, 0, 8, 2, ' ', 0)
	fmt.Fprintln(tw, "TARGET\tSTATUS\tEXPECTED\tRESULT\tDURATION\tERROR")

	healthy := 0
	for _, r := range rows {
		status := "-"
		if r.outcome.responded() {
			status = fmt.Sprint(r.statusCode)
		}
		if r.outcome == OutcomeOK {
			healthy++
		}
		detail := ""
		if r.errorData != nil {
			detail = r.errorData.Error()
		}
		fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\t%s\n",
			r.targetName, status, r.expectedStatus, r.outcome,
			r.duration.Round(time.Microsecond), detail)
	}

	if err := tw.Flush(); err != nil {
		return err
	}

	lines := strings.Split(strings.TrimSuffix(table.String(), "\n"), "\n")
	if _, err := fmt.Fprintln(w, paint(ansiBold, lines[0])); err != nil {
		return err
	}
	for i, line := range lines[1:] {
		if _, err := fmt.Fprintln(w, paint(rows[i].outcome.color(), line)); err != nil {
			return err
		}
	}

	summary := fmt.Sprintf("%d targets, %d ok, %d not ok", len(rows), healthy, len(rows)-healthy)
	summaryColor := ansiGreen
	if healthy < len(rows) {
		summaryColor = ansiRed
	}
	_, err := fmt.Fprintln(w, paint(summaryColor, summary))
	return err
}

type tableWriter struct {
	w         io.Writer
	tty       bool
	lastLines int
}

func newTableWriter(w io.Writer) *tableWriter {
	return &tableWriter{w: w, tty: isCharDevice(w)}
}

func isCharDevice(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func (t *tableWriter) render(results []Result) error {
	var buf bytes.Buffer
	if err := presentResults(&buf, results, time.Now(), t.tty); err != nil {
		return err
	}

	if t.tty && t.lastLines > 0 {
		if _, err := fmt.Fprintf(t.w, "\x1b[%dA\x1b[J", t.lastLines); err != nil {
			return err
		}
	}

	if _, err := t.w.Write(buf.Bytes()); err != nil {
		return err
	}

	if t.tty {
		t.lastLines = bytes.Count(buf.Bytes(), []byte("\n"))
	}
	return nil
}
