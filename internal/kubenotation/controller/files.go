// Copyright 2026 krax1337. Licensed under the Apache License, Version 2.0.

package controller

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
)

// validateFileName rejects names that are not a single path element, so a
// TrustPolicy/TrustStore can never write or delete outside the notation directory.
func validateFileName(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("invalid name %q: must be a single, non-empty path element", name)
	}
	return nil
}

// writeFileIfChanged atomically replaces path with data unless it already holds
// exactly data. The temporary file is created in tmpDir (same filesystem, not
// scanned by notation), so readers never observe a partially written file.
func writeFileIfChanged(path, tmpDir string, data []byte) (bool, error) {
	current, err := os.ReadFile(path)
	if err == nil && bytes.Equal(current, data) {
		return false, nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}

	tmp, err := os.CreateTemp(tmpDir, ".tmp-*")
	if err != nil {
		return false, err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return false, err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return false, err
	}
	if err := tmp.Close(); err != nil {
		return false, err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return false, err
	}
	return true, nil
}

// notify performs a non-blocking send; one pending notification is enough to
// make the verifier reload everything.
func notify(ch chan<- struct{}) {
	if ch == nil {
		return
	}
	select {
	case ch <- struct{}{}:
	default:
	}
}
