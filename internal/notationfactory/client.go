// Derived from kyverno-notation-verifier v1.1.0 by Nirmata (Apache-2.0), see internal/README.md.
// Modified by krax1337, 2026: fixed type-name typo, read-lock on the request
// path, per-request logging moved to debug level.

// Package notationfactory builds one notation verifier per trust policy file.
package notationfactory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/notaryproject/notation-go"
	"github.com/notaryproject/notation-go/dir"
	"github.com/notaryproject/notation-go/plugin"
	"github.com/notaryproject/notation-go/verifier"
	"github.com/notaryproject/notation-go/verifier/truststore"
	"go.uber.org/zap"

	"github.com/krax1337/notation-aws-verifier/internal/kubenotation/utils"
	"github.com/krax1337/notation-aws-verifier/internal/types"
)

type NotationVerifierFactory interface {
	// RefreshVerifiers will remove all the existing verifiers and create new ones using the trust policies in notation directory
	RefreshVerifiers() error

	// GetVerifier returns a verifier based on the trust policy in request or the default trustpolicy in trust policy env
	GetVerifier(requestData *types.VerificationRequest) (*notation.Verifier, error)
}

func NewNotationVerifierFactory(logger *zap.SugaredLogger) NotationVerifierFactory {
	return &notationverifierfactory{
		verifiers: make(map[string]*notation.Verifier),
		log:       logger,
	}
}

func (f *notationverifierfactory) RefreshVerifiers() error {
	f.lock.Lock()
	defer f.lock.Unlock()

	f.log.Info("Refreshing notation verifiers")
	verifiers := make(map[string]*notation.Verifier)

	entries, err := os.ReadDir(utils.NotationPath)
	if err != nil {
		f.log.Errorf("failed to read notation directory %v", err)
		return err
	}

	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			f.log.Debugf("Skipping entry in notation directory %s", e.Name())
			continue
		}

		fileName := filepath.Join(utils.NotationPath, e.Name())
		trustPolicy, err := f.loadTrustPolicy(fileName)
		if err != nil {
			f.log.Errorf("failed to load trust policy from file %s: %v", fileName, err)
			return err
		}
		f.log.Debugf("Trust policy loaded from file %s", fileName)

		x509TrustStore := truststore.NewX509TrustStore(dir.ConfigFS())

		v, err := verifier.New(trustPolicy, x509TrustStore, plugin.NewCLIManager(dir.PluginFS()))
		if err != nil {
			return err
		}
		trustpolicyName := strings.TrimSuffix(filepath.Base(fileName), filepath.Ext(fileName))

		verifiers[trustpolicyName] = &v
		f.log.Infof("Added verifier for trust policy %s", trustpolicyName)
	}

	f.verifiers = verifiers
	f.log.Infof("Successfully updated verifiers")
	return nil
}

func (f *notationverifierfactory) GetVerifier(requestData *types.VerificationRequest) (*notation.Verifier, error) {
	trustPolicy := requestData.TrustPolicy
	if len(trustPolicy) == 0 {
		trustPolicy = os.Getenv(types.EnvDefaultTrustPolicy)
		f.log.Debugf("Using default trust policy from env %s", trustPolicy)
	} else {
		f.log.Debugf("Using trust policy provided in the request %s", trustPolicy)
	}

	if len(trustPolicy) == 0 {
		return nil, fmt.Errorf("no trust policy specified, please specify a trust policy in request or set %s env", types.EnvDefaultTrustPolicy)
	}

	f.lock.RLock()
	defer f.lock.RUnlock()

	v, found := f.verifiers[trustPolicy]
	if !found {
		return nil, fmt.Errorf("no trust policy found for trust policy %s", trustPolicy)
	}

	return v, nil
}
