package ui

import (
	"crypto/sha256"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed assets/symphony-ui
var bundledExecutable []byte

// Extract writes the embedded platform UI executable into a private cache directory.
func Extract() (string, error) {
	if len(bundledExecutable) == 0 || string(bundledExecutable) == "This file is replaced by `make ui-build` with the platform OpenTUI executable.\n" {
		return "", fmt.Errorf("OpenTUI bundle is missing; rebuild Symphony with make build")
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("get user cache directory: %w", err)
	}
	sum := sha256.Sum256(bundledExecutable)
	directory := filepath.Join(cache, "symphony", "ui", fmt.Sprintf("%x", sum[:]))
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", fmt.Errorf("create UI cache directory: %w", err)
	}
	path := filepath.Join(directory, "symphony-ui")
	if info, err := os.Stat(path); err == nil && info.Mode().Perm() == 0o700 {
		return path, nil
	}
	temporary, err := os.CreateTemp(directory, ".symphony-ui-*")
	if err != nil {
		return "", fmt.Errorf("create UI executable: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if _, err := temporary.Write(bundledExecutable); err != nil {
		_ = temporary.Close()
		return "", fmt.Errorf("write UI executable: %w", err)
	}
	if err := temporary.Chmod(0o700); err != nil {
		_ = temporary.Close()
		return "", fmt.Errorf("set UI executable permissions: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return "", fmt.Errorf("close UI executable: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return "", fmt.Errorf("install UI executable: %w", err)
	}
	return path, nil
}
