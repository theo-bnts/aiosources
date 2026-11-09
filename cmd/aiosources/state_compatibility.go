package main

import (
	"errors"
	"os"
	"path/filepath"
)

const previousStateDirectory = ".ingest"

// Never silently replace an existing native deployment's key and password.
// Docker and explicit --data-dir/DATA_DIR values bypass this compatibility lookup.
func defaultStateDirectory(root string) (string, error) {
	current := filepath.Join(root, ".aiosources")
	previous := filepath.Join(root, previousStateDirectory)
	hasState := func(directory string) (bool, error) {
		dir, err := os.Lstat(directory)
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		if err != nil || !dir.IsDir() || dir.Mode()&os.ModeSymlink != 0 {
			return false, errors.New("private-state directory cannot be checked safely; choose --data-dir explicitly")
		}
		found := false
		for _, name := range []string{"vault.key", "admin-password"} {
			file, err := os.Lstat(filepath.Join(directory, name))
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil || !file.Mode().IsRegular() {
				return false, errors.New("private-state file cannot be checked safely; choose --data-dir explicitly")
			}
			found = true
		}
		return found, nil
	}
	currentState, err := hasState(current)
	if err != nil {
		return "", err
	}
	previousState, err := hasState(previous)
	if err != nil {
		return "", err
	}
	if currentState && previousState {
		return "", errors.New("both private-state directories contain application credentials; choose --data-dir explicitly")
	}
	if previousState {
		return previous, nil
	}
	return current, nil
}
