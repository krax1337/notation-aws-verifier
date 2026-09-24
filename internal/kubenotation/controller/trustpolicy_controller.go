/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Derived from kyverno-notation-verifier v1.1.0 by Nirmata (Apache-2.0), see internal/README.md.
// Modified by krax1337, 2026: every reconcile rebuilds the trust policy files
// from the full TrustPolicy list, so replicas that did not remove the finalizer
// also delete stale files; write errors are no longer ignored; files are written
// atomically; names are validated; the verifier is only notified on changes.

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	notationv1alpha1 "github.com/krax1337/notation-aws-verifier/internal/kubenotation/api/v1alpha1"
	"github.com/krax1337/notation-aws-verifier/internal/kubenotation/utils"
)

// TrustPolicyReconciler reconciles TrustPolicy objects into trust policy files
// in utils.NotationPath.
type TrustPolicyReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	// CRDChangeInformer receives a notification whenever the files on disk change.
	CRDChangeInformer chan<- struct{}
}

//+kubebuilder:rbac:groups=notation.nirmata.io,resources=trustpolicies,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=notation.nirmata.io,resources=trustpolicies/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=notation.nirmata.io,resources=trustpolicies/finalizers,verbs=update

// Reconcile keeps the finalizer on live objects and rewrites the trust policy
// directory from the list of all TrustPolicies.
func (r *TrustPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var trustPolicy notationv1alpha1.TrustPolicy
	found := true
	if err := r.Get(ctx, req.NamespacedName, &trustPolicy); err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		// Deleted, possibly finalized by another replica: the sync below
		// removes its file from this replica's disk.
		found = false
	}

	if found && trustPolicy.DeletionTimestamp.IsZero() && controllerutil.AddFinalizer(&trustPolicy, utils.FinalizerName) {
		if err := r.Update(ctx, &trustPolicy); err != nil {
			return ctrl.Result{}, err
		}
	}

	if err := r.sync(ctx, logger); err != nil {
		return ctrl.Result{}, err
	}

	if found && !trustPolicy.DeletionTimestamp.IsZero() && controllerutil.RemoveFinalizer(&trustPolicy, utils.FinalizerName) {
		if err := r.Update(ctx, &trustPolicy); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *TrustPolicyReconciler) sync(ctx context.Context, logger logr.Logger) error {
	var list notationv1alpha1.TrustPolicyList
	if err := r.List(ctx, &list); err != nil {
		return fmt.Errorf("failed to list trust policies: %w", err)
	}

	desired := make(map[string][]byte, len(list.Items))
	for i := range list.Items {
		policy := &list.Items[i]
		if !policy.DeletionTimestamp.IsZero() {
			continue
		}
		if err := validateFileName(policy.Spec.TrustPolicyName); err != nil {
			logger.Error(err, "skipping TrustPolicy", "name", policy.Name)
			continue
		}
		data, err := json.MarshalIndent(policy.Spec, "  ", " ")
		if err != nil {
			return fmt.Errorf("failed to marshal trust policy %s: %w", policy.Name, err)
		}
		desired[policy.Spec.TrustPolicyName+".json"] = data
	}

	changed, err := syncTrustPolicyFiles(utils.NotationPath, desired)
	if err != nil {
		return err
	}
	if changed {
		logger.Info("trust policies updated", "count", len(desired))
		notify(r.CRDChangeInformer)
	}
	return nil
}

// syncTrustPolicyFiles makes the *.json files in dir exactly match desired
// (file name -> content).
func syncTrustPolicyFiles(dir string, desired map[string][]byte) (bool, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false, fmt.Errorf("failed to create %s: %w", dir, err)
	}

	changed := false
	for name, data := range desired {
		written, err := writeFileIfChanged(filepath.Join(dir, name), dir, data)
		if err != nil {
			return changed, fmt.Errorf("failed to write trust policy %s: %w", name, err)
		}
		changed = changed || written
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return changed, err
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		if _, ok := desired[e.Name()]; ok {
			continue
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil && !os.IsNotExist(err) {
			return changed, fmt.Errorf("failed to delete trust policy %s: %w", e.Name(), err)
		}
		changed = true
	}
	return changed, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *TrustPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&notationv1alpha1.TrustPolicy{}).
		Complete(r)
}
