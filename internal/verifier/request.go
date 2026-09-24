// Derived from kyverno-notation-verifier v1.1.0 by Nirmata (Apache-2.0), see internal/README.md.
// Modified by krax1337, 2026: moved from verifier/internal into this package;
// the caller-supplied "metadata" annotation is no longer parsed because it was
// only used to skip verification (see verify.go).

package verifier

import (
	"errors"
	"fmt"

	"github.com/krax1337/notation-aws-verifier/internal/types"
)

func processRequestData(req *types.RequestData) (*types.VerificationRequest, error) {
	if len(req.Images.Containers) == 0 &&
		len(req.Images.InitContainers) == 0 &&
		len(req.Images.EphemeralContainers) == 0 {
		return nil, errors.New("at least one image must be provided")
	}
	for _, att := range req.Attestations {
		if att.ImageReference == "" {
			return nil, fmt.Errorf("image reference cannot be empty %+v", att)
		}

		for _, attType := range att.Type {
			if attType.Name == "" {
				return nil, errors.New("attestation name cannot be empty")
			}
			for _, cond := range attType.Conditions.AnyConditions {
				if cond.RawKey == nil {
					return nil, errors.New("condition key cannot be empty")
				}

				if cond.RawValue == nil {
					return nil, errors.New("condition value cannot be empty")
				}
			}

			for _, cond := range attType.Conditions.AllConditions {
				if cond.RawKey == nil {
					return nil, errors.New("condition key cannot be empty")
				}

				if cond.RawValue == nil {
					return nil, errors.New("condition value cannot be empty")
				}
			}
		}
	}

	return &types.VerificationRequest{
		ImageReferences: req.ImageReferences,
		Images:          req.Images,
		Attestations:    req.Attestations,
		TrustPolicy:     req.TrustPolicy,
		Insecure:        req.Insecure,
	}, nil
}
