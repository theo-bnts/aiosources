package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestReadPrivateFileRejectsFIFOWithoutWriter(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "vault.key")
	if err := syscall.Mkfifo(filename, 0600); err != nil {
		t.Fatal(err)
	}
	type result struct {
		data []byte
		err  error
	}
	finished := make(chan result, 1)
	go func() {
		data, err := readPrivateFile(filename)
		finished <- result{data: data, err: err}
	}()
	select {
	case got := <-finished:
		if got.err == nil || len(got.data) != 0 {
			t.Fatal("a FIFO was accepted as a private state file")
		}
	case <-time.After(2 * time.Second):
		// Release a blocked reader before failing the regression.
		if file, err := os.OpenFile(filename, os.O_RDWR|syscall.O_NONBLOCK, 0); err == nil {
			defer file.Close()
			select {
			case <-finished:
			case <-time.After(2 * time.Second):
			}
		}
		t.Fatal("private state read blocked waiting for a FIFO writer")
	}
}
