package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultStateDirectoryKeepsExistingNativeCredentials(t *testing.T) {
	for _, name := range []string{"vault.key", "admin-password"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			previous := filepath.Join(root, previousStateDirectory)
			if err := os.Mkdir(previous, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(previous, name), []byte("synthetic-preserved-value"), 0600); err != nil {
				t.Fatal(err)
			}
			selected, err := defaultStateDirectory(root)
			if err != nil || selected != previous {
				t.Fatal("existing state was replaced", err)
			}
			if _, err := os.Stat(filepath.Join(root, ".aiosources")); !os.IsNotExist(err) {
				t.Fatal("compatibility lookup generated a new state directory")
			}
		})
	}
}

func TestDefaultStateDirectoryDoesNotMovePrivatePerformanceWorkspaces(t *testing.T) {
	root := t.TempDir()
	previous := filepath.Join(root, previousStateDirectory)
	if err := os.MkdirAll(filepath.Join(previous, "performance"), 0700); err != nil {
		t.Fatal(err)
	}
	selected, err := defaultStateDirectory(root)
	if err != nil || selected != filepath.Join(root, ".aiosources") {
		t.Fatal("a diagnostic workspace was mistaken for application credentials", err)
	}
}

func TestDefaultStateDirectoryRejectsAmbiguousCredentialsAndSymlinks(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{".aiosources", previousStateDirectory} {
		folder := filepath.Join(root, name)
		if err := os.Mkdir(folder, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(folder, "vault.key"), []byte("synthetic"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := defaultStateDirectory(root); err == nil {
		t.Fatal("two vaults were selected implicitly")
	}
	other := t.TempDir()
	if err := os.Symlink(root, filepath.Join(other, previousStateDirectory)); err != nil {
		t.Fatal(err)
	}
	if _, err := defaultStateDirectory(other); err == nil {
		t.Fatal("a symbolic state directory was accepted implicitly")
	}
}
