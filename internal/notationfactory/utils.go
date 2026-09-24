// Derived from kyverno-notation-verifier v1.1.0 by Nirmata (Apache-2.0), see internal/README.md.
// Modified by krax1337, 2026: RWMutex, dropped the redundant clear(), keep the
// decode error in the malformed-policy message.

package notationfactory

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sync"

	"github.com/notaryproject/notation-go"
	"github.com/notaryproject/notation-go/verifier/trustpolicy"
	"go.uber.org/zap"
)

type notationverifierfactory struct {
	verifiers map[string]*notation.Verifier
	log       *zap.SugaredLogger
	lock      sync.RWMutex
}

func (f *notationverifierfactory) loadTrustPolicy(path string) (*trustpolicy.Document, error) {
	fileInfo, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("trust policy is not present %s", path)
		}
		return nil, err
	}

	mode := fileInfo.Mode()
	if mode.IsDir() || mode&fs.ModeSymlink != 0 {
		return nil, fmt.Errorf("trust policy is not a regular file (symlinks are not supported) path: %s", path)
	}

	jsonFile, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return nil, fmt.Errorf("unable to read trust policy due to file permissions, please verify the permissions of %s", path)
		}
		return nil, err
	}
	defer func() { _ = jsonFile.Close() }()

	policyDocument := &trustpolicy.Document{}
	if err := json.NewDecoder(jsonFile).Decode(policyDocument); err != nil {
		return nil, fmt.Errorf("malformed trust policy path %s: %w", path, err)
	}
	return policyDocument, nil
}
