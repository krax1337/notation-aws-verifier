// Derived from kyverno-notation-verifier v1.1.0 by Nirmata (Apache-2.0), see internal/README.md.
// Modified by krax1337, 2026: merged setup/internal into this package; return
// errors instead of calling log.Fatalf.

// Package setup prepares the local notation configuration directory.
package setup

import (
	"fmt"
	"os"

	"github.com/notaryproject/notation-go/dir"
	"go.uber.org/zap"
)

// Local takes NOTATION_DIR and PLUGINS_DIR from env, installs the
// notation plugins into NOTATION_DIR and points notation at that directory.
func Local(logger *zap.SugaredLogger) error {
	if err := installPlugins(); err != nil {
		return fmt.Errorf("failed to install plugins: %w", err)
	}

	installDir := os.Getenv("NOTATION_DIR")
	dir.UserConfigDir = installDir
	dir.UserLibexecDir = installDir
	logger.Infow("configuring notation", "dir.UserConfigDir", dir.UserConfigDir, "dir.UserLibexecDir", dir.UserLibexecDir)
	return nil
}
