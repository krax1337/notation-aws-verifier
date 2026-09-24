// Derived from kyverno-notation-verifier v1.1.0 by Nirmata (Apache-2.0), see internal/README.md.
// Modified by krax1337, 2026: moved from setup/internal; the copy no longer
// aborts on already existing directories (container restarts with a persisted
// emptyDir), is confined to the source/destination trees with os.Root and
// installs plugin binaries with 0755 instead of world-writable 0777.

package setup

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// pluginPerm is used for plugin directories and binaries: notation executes the
// plugins, so they must be executable; nothing else needs write access.
const pluginPerm fs.FileMode = 0o755

func installPlugins() error {
	sourceDir := os.Getenv("PLUGINS_DIR")
	if sourceDir == "" {
		return errors.New("missing PLUGINS_DIR")
	}

	notationDir := os.Getenv("NOTATION_DIR")
	if notationDir == "" {
		return errors.New("missing NOTATION_DIR")
	}

	destinationDir := filepath.Join(notationDir, "plugins")
	if err := os.MkdirAll(destinationDir, pluginPerm); err != nil { //nolint:gosec // plugin directory must be traversable by notation
		return err
	}

	if err := copyTree(sourceDir, destinationDir); err != nil {
		return fmt.Errorf("failed to copy %s to %s: %w", sourceDir, destinationDir, err)
	}

	return nil
}

// copyTree copies the regular files and directories below source into
// destination, overwriting existing files.
func copyTree(source, destination string) error {
	src, err := os.OpenRoot(source)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()

	dst, err := os.OpenRoot(destination)
	if err != nil {
		return err
	}
	defer func() { _ = dst.Close() }()

	return fs.WalkDir(src.FS(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == "." {
			return nil
		}
		if d.IsDir() {
			return dst.MkdirAll(path, pluginPerm)
		}
		data, err := src.ReadFile(path)
		if err != nil {
			return err
		}
		if err := dst.WriteFile(path, data, pluginPerm); err != nil {
			return err
		}
		// WriteFile does not change the mode of an existing file.
		return dst.Chmod(path, pluginPerm)
	})
}
