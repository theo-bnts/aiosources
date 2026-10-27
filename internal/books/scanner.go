package books

import (
	"bufio"
	"errors"
	"io"
)

const maxExportRecord = 8 << 20

// ExportScanner bounds memory even for abnormally large historical records.
// It drains an oversized line so later valid records can still be imported.
// Network and gzip integrity errors remain fatal, never successful completion.
type ExportScanner struct {
	reader  *bufio.Reader
	line    []byte
	skipped bool
	done    bool
	err     error
}

func NewExportScanner(reader io.Reader) *ExportScanner {
	return &ExportScanner{reader: bufio.NewReaderSize(reader, 64<<10)}
}
func (s *ExportScanner) Scan() bool {
	if s.done {
		return false
	}
	s.line = s.line[:0]
	s.skipped = false
	for {
		chunk, err := s.reader.ReadSlice('\n')
		if len(s.line)+len(chunk) > maxExportRecord {
			s.skipped = true
			s.line = s.line[:0]
		}
		if !s.skipped {
			s.line = append(s.line, chunk...)
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if errors.Is(err, io.EOF) {
			s.done = true
			return len(s.line) > 0 || s.skipped
		}
		if err != nil {
			s.err = err
			s.done = true
			return false
		}
		return true
	}
}
func (s *ExportScanner) Bytes() []byte { return s.line }
func (s *ExportScanner) Skipped() bool { return s.skipped }
func (s *ExportScanner) Err() error    { return s.err }
