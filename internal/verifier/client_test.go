package verifier

import (
	"context"
	"net/http"
	"slices"
	"testing"

	"go.uber.org/zap"
	authv1 "k8s.io/api/authentication/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestAuthenticate(t *testing.T) {
	const allowed = "system:serviceaccount:kyverno:kyverno-admission-controller"

	tests := []struct {
		name       string
		header     string
		audiences  []string
		username   string
		authn      bool
		wantStatus int
		wantReview bool
	}{
		{name: "missing header", header: "", wantStatus: http.StatusUnauthorized},
		{name: "no bearer prefix", header: "Basic dXNlcjpwYXNz", wantStatus: http.StatusUnauthorized},
		{name: "empty bearer token", header: "Bearer ", wantStatus: http.StatusUnauthorized},
		{name: "unauthenticated", header: "Bearer t", authn: false, wantStatus: http.StatusUnauthorized, wantReview: true},
		{name: "user not allowed", header: "Bearer t", authn: true, username: "system:anonymous", wantStatus: http.StatusForbidden, wantReview: true},
		{name: "allowed", header: "Bearer t", authn: true, username: allowed, wantStatus: http.StatusOK, wantReview: true},
		{name: "allowed with audiences", header: "Bearer t", audiences: []string{"kyverno-svc.kyverno.io"}, authn: true, username: allowed, wantStatus: http.StatusOK, wantReview: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewClientset()
			var reviewed *authv1.TokenReview
			client.PrependReactor("create", "tokenreviews", func(action k8stesting.Action) (bool, runtime.Object, error) {
				reviewed = action.(k8stesting.CreateAction).GetObject().(*authv1.TokenReview).DeepCopy()
				out := reviewed.DeepCopy()
				out.Status = authv1.TokenReviewStatus{Authenticated: tt.authn, User: authv1.UserInfo{Username: tt.username}}
				return true, out, nil
			})

			v := &verifier{
				logger:               zap.NewNop().Sugar(),
				kubeClient:           client,
				allowedUsers:         []string{allowed},
				tokenReviewAudiences: tt.audiences,
			}

			status, _ := v.authenticate(context.Background(), tt.header)
			if status != tt.wantStatus {
				t.Fatalf("status = %d, want %d", status, tt.wantStatus)
			}
			if (reviewed != nil) != tt.wantReview {
				t.Fatalf("token reviewed = %v, want %v", reviewed != nil, tt.wantReview)
			}
			if reviewed == nil {
				return
			}
			if reviewed.Spec.Token != "t" {
				t.Errorf("reviewed token = %q, want %q", reviewed.Spec.Token, "t")
			}
			if !slices.Equal(reviewed.Spec.Audiences, tt.audiences) {
				t.Errorf("reviewed audiences = %v, want %v", reviewed.Spec.Audiences, tt.audiences)
			}
		})
	}
}
