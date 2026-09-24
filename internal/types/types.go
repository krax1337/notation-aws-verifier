// Derived from kyverno-notation-verifier v1.1.0 by Nirmata (Apache-2.0), see internal/README.md.
// Modified by krax1337, 2026: removed unused CertFile/KeyFile, fixed typos.

// Package types holds the request/response wire format of the /checkimages endpoint.
package types

import (
	kyvernov1 "github.com/kyverno/kyverno/api/kyverno/v1"
	imageutils "github.com/kyverno/kyverno/pkg/utils/image"
	"gomodules.xyz/jsonpatch/v2"
)

// EnvDefaultTrustPolicy names the environment variable holding the trust
// policy used when a request does not specify one.
const EnvDefaultTrustPolicy = "DEFAULT_TRUST_POLICY"

type ImageInfo struct {
	Image

	// Pointer is the path to the image object in the resource
	Pointer string `json:"jsonPointer"`
}

type Image struct {
	imageutils.ImageInfo
}

type AttestationType struct {
	// Name is the media type of the attestation
	Name string `json:"name"`

	// Conditions are used to determine if a policy rule should be applied by evaluating a
	// set of conditions. The declaration can contain nested `any` or `all` statements.
	Conditions kyvernov1.AnyAllConditions `json:"conditions"`
}

type AttestationsInfo struct {
	// Image references are the regex of the images containing these attestations
	ImageReference string `json:"imageReference"`

	// type is a list of all the attestation types to check in these images
	Type []AttestationType `json:"type"`
}

type ImageInfos struct {
	// InitContainers is a map of init containers image data from the AdmissionReview request, key is the container name
	InitContainers map[string]ImageInfo `json:"initContainers,omitempty"`

	// Containers is a map of containers image data from the AdmissionReview request, key is the container name
	Containers map[string]ImageInfo `json:"containers,omitempty"`

	// EphemeralContainers is a map of ephemeral containers image data from the AdmissionReview request, key is the container name
	EphemeralContainers map[string]ImageInfo `json:"ephemeralContainers,omitempty"`
}

// Data format of request body for HandleCheckImages
type RequestData struct {
	// ImageReferences is a list of matching image reference patterns. At least one pattern in the
	// list must match the image for the rule to apply. Each image reference consists of a registry
	// address (defaults to docker.io), repository, image, and tag (defaults to latest).
	// Wildcards ('*' and '?') are allowed. See: https://kubernetes.io/docs/concepts/containers/images.
	// +kubebuilder:validation:Optional
	ImageReferences []string `json:"imageReferences"`

	// List of images in the form of kyverno's image variable
	Images ImageInfos `json:"images"`

	// TrustPolicy specifies the name of the trust policy to be used for this specific request
	TrustPolicy string `json:"trustPolicy"`

	// List of image regex and attestations
	Attestations []AttestationsInfo `json:"attestations"`

	// Metadata is the value of the kyverno-notation-aws.io/verify-images annotation.
	//
	// Deprecated: accepted for compatibility and ignored. It was used to skip
	// verification of images listed as verified, but that annotation is caller
	// controlled and never written by this service.
	Metadata string `json:"metadata"`

	// Insecure allows insecure access to the registry
	Insecure bool `json:"insecure"`
}

// VerificationRequest is the data sent to verifier after processed from HandleCheckImages request
type VerificationRequest struct {
	// ImageReferences is a list of matching image reference patterns. At least one pattern in the
	// list must match the image for the rule to apply. Each image reference consists of a registry
	// address (defaults to docker.io), repository, image, and tag (defaults to latest).
	// Wildcards ('*' and '?') are allowed. See: https://kubernetes.io/docs/concepts/containers/images.
	// +kubebuilder:validation:Optional
	ImageReferences []string `json:"imageReferences"`

	// List of images in the form of kyverno's image variable
	Images ImageInfos `json:"images"`

	// TrustPolicy specifies the name of the trust policy to be used for this specific request
	TrustPolicy string `json:"trustPolicy"`

	// List of image regex and attestations
	Attestations []AttestationsInfo `json:"attestations"`

	// Insecure allows insecure access to the registry
	Insecure bool `json:"insecure"`
}

// Data format of response body for HandleCheckImages
type ResponseData struct {
	// Verified is true when all the images are verified.
	Verified bool `json:"verified"`

	// ErrorMessage contains the error received when verification fails
	// ErrorMessage is empty when verification succeeds
	ErrorMessage string `json:"message,omitempty"`

	// Results contains the list of containers in JSONPatch format
	// Results is empty when verification fails
	Results []jsonpatch.Operation `json:"results"`
}

type AttestationList map[string][]kyvernov1.AnyAllConditions
