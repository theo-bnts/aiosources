package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func privateDirectory(directory string) error {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return errors.New("could not create private state directory")
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("state path must be a real directory, not a symbolic link")
	}
	if info.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("state directory must be private (chmod 700 %q)", directory)
	}
	return nil
}

func readPrivateFile(filename string) ([]byte, error) {
	file, err := os.OpenFile(filename, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, os.ErrNotExist
		}
		return nil, errors.New("could not open private state file")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("private state file must be regular and readable only by its owner")
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); !ok || stat.Nlink != 1 {
		return nil, errors.New("private state file must not be hard-linked")
	}
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil || len(data) > 4096 {
		return nil, errors.New("invalid private state file")
	}
	return data, nil
}

// Exclusive creation never replaces a key. A partially written file fails closed
// on the next startup instead of silently changing the vault's identity.
func createPrivateFile(filename string, data []byte) ([]byte, error) {
	file, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0600)
	if errors.Is(err, os.ErrExist) {
		return readPrivateFile(filename)
	}
	if err != nil {
		return nil, errors.New("could not create private state file")
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return nil, errors.New("could not write private state file")
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return nil, errors.New("could not flush private state file")
	}
	if err := file.Close(); err != nil {
		return nil, errors.New("could not close private state file")
	}
	directory, err := os.Open(filepath.Dir(filename))
	if err != nil {
		return nil, errors.New("could not open private state directory")
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return nil, errors.New("could not flush private state directory")
	}
	return data, nil
}

func decodeMasterKey(encoded string) ([]byte, error) {
	encoded = strings.TrimSpace(encoded)
	if len(encoded) == 64 {
		if value, err := hex.DecodeString(encoded); err == nil {
			return value, nil
		}
	}
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if value, err := encoding.DecodeString(encoded); err == nil && len(value) == 32 {
			return value, nil
		}
	}
	return nil, errors.New("master key must encode exactly 32 bytes (base64 or hexadecimal); existing keys are never regenerated")
}

func masterKey(directory string) ([]byte, error) {
	if encoded := os.Getenv("MASTER_KEY"); encoded != "" {
		return decodeMasterKey(encoded)
	}
	filename := filepath.Join(directory, "vault.key")
	data, err := readPrivateFile(filename)
	if errors.Is(err, os.ErrNotExist) {
		var key [32]byte
		if _, err := rand.Read(key[:]); err != nil {
			return nil, errors.New("could not generate master key")
		}
		encoded := base64.StdEncoding.EncodeToString(key[:]) + "\n"
		clear(key[:])
		data, err = createPrivateFile(filename, []byte(encoded))
	}
	if err != nil {
		return nil, err
	}
	defer clear(data)
	return decodeMasterKey(string(data))
}

func administratorPassword(directory string) (string, error) {
	password := os.Getenv("ADMIN_PASSWORD")
	if password == "" {
		filename := filepath.Join(directory, "admin-password")
		data, err := readPrivateFile(filename)
		if errors.Is(err, os.ErrNotExist) {
			var random [32]byte
			if _, err := rand.Read(random[:]); err != nil {
				return "", errors.New("could not generate administrator password")
			}
			data, err = createPrivateFile(filename, []byte(base64.RawURLEncoding.EncodeToString(random[:])+"\n"))
			clear(random[:])
		}
		if err != nil {
			return "", err
		}
		password = strings.TrimSuffix(string(data), "\n")
		clear(data)
	}
	if len(password) < 12 || len(password) > 72 {
		return "", errors.New("administrator password must contain 12 to 72 bytes")
	}
	return password, nil
}
