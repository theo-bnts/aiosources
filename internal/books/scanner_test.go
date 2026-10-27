package books

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestExportScannerPreservesLaterRecordsAndBoundsLargeLines(t *testing.T) {
	scanner := NewExportScanner(strings.NewReader("first\n" + strings.Repeat("x", maxExportRecord+100) + "\nlast"))
	if !scanner.Scan() || string(scanner.Bytes()) != "first\n" || scanner.Skipped() {
		t.Fatal("first line lost")
	}
	if !scanner.Scan() || !scanner.Skipped() || len(scanner.Bytes()) != 0 {
		t.Fatal("oversized line retained")
	}
	if !scanner.Scan() || string(scanner.Bytes()) != "last" || scanner.Skipped() {
		t.Fatal("later valid line lost")
	}
	if scanner.Scan() || scanner.Err() != nil {
		t.Fatal("clean EOF failed")
	}
}

type brokenExportReader struct{}

func (brokenExportReader) Read(p []byte) (int, error) {
	copy(p, "partial")
	return min(len(p), 7), io.ErrUnexpectedEOF
}
func TestExportScannerNeverAcceptsAnInterruptedRecord(t *testing.T) {
	s := NewExportScanner(brokenExportReader{})
	if s.Scan() || !errors.Is(s.Err(), io.ErrUnexpectedEOF) {
		t.Fatal("partial transfer accepted")
	}
}
