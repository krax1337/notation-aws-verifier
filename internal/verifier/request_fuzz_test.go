package verifier

import (
	"encoding/json"
	"testing"

	"github.com/krax1337/notation-aws-verifier/internal/types"
)

// FuzzProcessRequestData feeds arbitrary /checkimages request bodies through the
// same decode and validation path as the HTTP handler. The body is supplied by
// the caller (a Kyverno policy), so malformed input must be rejected with an
// error, never a panic, and an accepted request must carry at least one image.
func FuzzProcessRequestData(f *testing.F) {
	seeds := []string{
		`{}`,
		`{"images":{}}`,
		`{"images":{"containers":{"app":{"registry":"123456789012.dkr.ecr.us-west-2.amazonaws.com","path":"demo","name":"demo","tag":"v1","jsonPointer":"/spec/containers/0/image"}}}}`,
		`{"images":{"initContainers":{"init":{"registry":"public.ecr.aws","path":"a/b","name":"b","digest":"sha256:00"}}},"trustPolicy":"tp-default"}`,
		`{"images":{"containers":{"a":{}}},"attestations":[{"imageReference":"*","type":[{"name":"sbom/cyclone-dx","conditions":{"all":[{"key":"{{a}}","operator":"Equals","value":"1"}]}}]}]}`,
		`{"images":{"containers":{"a":{}}},"attestations":[{"imageReference":"","type":[]}]}`,
		`{"images":{"containers":{"a":{}}},"attestations":[{"imageReference":"*","type":[{"name":"x","conditions":{"any":[{"operator":"Equals"}]}}]}]}`,
		`{"images":null,"attestations":null,"imageReferences":["*"],"insecure":true}`,
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, body []byte) {
		var req types.RequestData
		if err := json.Unmarshal(body, &req); err != nil {
			return
		}
		got, err := processRequestData(&req)
		if err != nil {
			if got != nil {
				t.Fatalf("non-nil request returned with error %v", err)
			}
			return
		}
		if got == nil {
			t.Fatal("nil request returned without error")
		}
		if len(got.Images.Containers)+len(got.Images.InitContainers)+len(got.Images.EphemeralContainers) == 0 {
			t.Fatal("request without images was accepted")
		}
	})
}
