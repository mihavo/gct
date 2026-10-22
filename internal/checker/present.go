package checker

import (
	"bytes"
	"cmp"
	"fmt"
	"io"
	"os"
	"slices"
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
	default:
		return fmt.Sprintf("Outcome(%d)", int(o))
	}
}

func (o Outcome) responded() bool {
	return o == OutcomeOK || o == OutcomeFail
}

const roundTimeLayout = "2006-01-02 15:04:05"

func presentResults(w io.Writer, results []Result, now time.Time) error {
	if _, err := fmt.Fprintf(w, "round at %s\n", now.Format(roundTimeLayout)); err != nil {
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

	tw := tabwriter.NewWriter(w, 0, 8, 2, ' ', 0)
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

	_, err := fmt.Fprintf(w, "%d targets, %d ok, %d not ok\n",
		len(rows), healthy, len(rows)-healthy)
	return err
}

type tableWriter struct {
	w         io.Writer
	inPlace   bool
	lastLines int
}

func newTableWriter(w io.Writer) *tableWriter {
	return &tableWriter{w: w, inPlace: isCharDevice(w)}
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
	if err := presentResults(&buf, results, time.Now()); err != nil {
		return err
	}

	if t.inPlace && t.lastLines > 0 {
		if _, err := fmt.Fprintf(t.w, "\x1b[%dA\x1b[J", t.lastLines); err != nil {
			return err
		}
	}

	if _, err := t.w.Write(buf.Bytes()); err != nil {
		return err
	}

	if t.inPlace {
		t.lastLines = bytes.Count(buf.Bytes(), []byte("\n"))
	}
	return nil
}
