// Derived from kyverno-notation-verifier v1.1.0 by Nirmata (Apache-2.0), see internal/README.md.
// Modified by krax1337, 2026: removed the unused annotation metadata, dropped
// the dead annotation patch code, per-request logs at debug level.

package verifier

import (
	"errors"

	kyvernov1 "github.com/kyverno/kyverno/api/kyverno/v1"
	"github.com/kyverno/kyverno/ext/wildcard"
	"go.uber.org/zap"
	"gomodules.xyz/jsonpatch/v2"

	"github.com/krax1337/notation-aws-verifier/internal/types"
)

type Response interface {
	GetResponse() types.ResponseData
	GetImageList() map[string]types.AttestationList
	AddImage(imageRef string, img *types.ImageInfo)
	BuildAttestationList(Attestations []types.AttestationsInfo) error
	VerificationFailed(msg string) (types.ResponseData, error)
	VerificationSucceeded(msg string) (types.ResponseData, error)
}

type responseStruct struct {
	log          *zap.SugaredLogger
	imageList    map[string]types.AttestationList
	responseData types.ResponseData
}

func NewResponse(log *zap.SugaredLogger) Response {
	return &responseStruct{
		log:       log,
		imageList: make(map[string]types.AttestationList),
		responseData: types.ResponseData{
			Verified: true,
			Results:  make([]jsonpatch.Operation, 0),
		},
	}
}

func (r *responseStruct) GetResponse() types.ResponseData {
	return r.responseData
}

func (r *responseStruct) GetImageList() map[string]types.AttestationList {
	return r.imageList
}

func (r *responseStruct) AddImage(imageRef string, img *types.ImageInfo) {
	imageData := jsonpatch.Operation{
		Operation: "replace",
		Path:      img.Pointer,
		Value:     img.String(),
	}

	r.responseData.Results = append(r.responseData.Results, imageData)
	r.imageList[imageRef] = make(types.AttestationList)
}

func (r *responseStruct) addAttestations(img string, att types.AttestationType) error {
	r.log.Debugf("Adding attestations %s %v", img, att)
	if _, found := r.imageList[img]; !found {
		return errors.New("image not found in image list")
	}
	if _, ok := r.imageList[img][att.Name]; !ok {
		r.imageList[img][att.Name] = make([]kyvernov1.AnyAllConditions, 0)
	}
	if len(att.Conditions.AllConditions) != 0 || len(att.Conditions.AnyConditions) != 0 {
		r.imageList[img][att.Name] = append(r.imageList[img][att.Name], att.Conditions)
	}
	return nil
}

func (r *responseStruct) VerificationFailed(msg string) (types.ResponseData, error) {
	r.log.Warnf("Verification failed with error %s", msg)
	r.responseData.Verified = false
	r.responseData.ErrorMessage = msg
	r.responseData.Results = make([]jsonpatch.Operation, 0)

	return r.responseData, nil
}

func (r *responseStruct) VerificationSucceeded(msg string) (types.ResponseData, error) {
	r.responseData.ErrorMessage = msg
	r.log.Debugf("Sending response result=%+v", r.responseData.Results)
	return r.responseData, nil
}

func (r *responseStruct) BuildAttestationList(attestations []types.AttestationsInfo) error {
	r.log.Debugf("building attestation set %v", attestations)
	for _, attestation := range attestations {
		for image := range r.imageList {
			if wildcard.Match(attestation.ImageReference, image) {
				for _, attestationType := range attestation.Type {
					if err := r.addAttestations(image, attestationType); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}
