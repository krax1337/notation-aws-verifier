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
// Modified by krax1337, 2026: every reconcile rebuilds the trust store
// directories from the full TrustStore list, so replicas that did not remove
// the finalizer also delete stale stores; write errors are no longer ignored;
// files are written atomically; names are validated; the verifier is only
// notified on changes.

package controller

import (
	"context"
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

const trustStoreCertFile = "certificates.crt"

// TrustStoreReconciler reconciles TrustStore objects into notation x509 trust
// store directories below utils.NotationPath.
type TrustStoreReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	// CRDChangeInformer receives a notification whenever the files on disk change.
	CRDChangeInformer chan<- struct{}
}

//+kubebuilder:rbac:groups=notation.nirmata.io,resources=truststores,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=notation.nirmata.io,resources=truststores/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=notation.nirmata.io,resources=truststores/finalizers,verbs=update

// Reconcile keeps the finalizer on live objects and rewrites the trust store
// directories from the list of all TrustStores.
func (r *TrustStoreReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var trustStore notationv1alpha1.TrustStore
	found := true
	if err := r.Get(ctx, req.NamespacedName, &trustStore); err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		// Deleted, possibly finalized by another replica: the sync below
		// removes its directory from this replica's disk.
		found = false
	}

	if found && trustStore.DeletionTimestamp.IsZero() && controllerutil.AddFinalizer(&trustStore, utils.FinalizerName) {
		if err := r.Update(ctx, &trustStore); err != nil {
			return ctrl.Result{}, err
		}
	}

	if err := r.sync(ctx, logger); err != nil {
		return ctrl.Result{}, err
	}

	if found && !trustStore.DeletionTimestamp.IsZero() && controllerutil.RemoveFinalizer(&trustStore, utils.FinalizerName) {
		if err := r.Update(ctx, &trustStore); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *TrustStoreReconciler) sync(ctx context.Context, logger logr.Logger) error {
	var list notationv1alpha1.TrustStoreList
	if err := r.List(ctx, &list); err != nil {
		return fmt.Errorf("failed to list trust stores: %w", err)
	}

	// desired maps "<type>/<name>" to the PEM bundle.
	desired := make(map[string][]byte, len(list.Items))
	for i := range list.Items {
		store := &list.Items[i]
		if !store.DeletionTimestamp.IsZero() {
			continue
		}
		if err := validateFileName(store.Spec.Type); err != nil {
			logger.Error(err, "skipping TrustStore", "name", store.Name)
			continue
		}
		if err := validateFileName(store.Spec.TrustStoreName); err != nil {
			logger.Error(err, "skipping TrustStore", "name", store.Name)
			continue
		}
		desired[filepath.Join(store.Spec.Type, store.Spec.TrustStoreName)] = []byte(store.Spec.CABundle)
	}

	changed, err := syncTrustStores(filepath.Join(utils.NotationPath, utils.TrustStorePath), desired)
	if err != nil {
		return err
	}
	if changed {
		logger.Info("trust stores updated", "count", len(desired))
		notify(r.CRDChangeInformer)
	}
	return nil
}

// syncTrustStores makes the <root>/<type>/<name>/certificates.crt tree exactly
// match desired ("<type>/<name>" -> PEM bundle).
func syncTrustStores(root string, desired map[string][]byte) (bool, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return false, fmt.Errorf("failed to create %s: %w", root, err)
	}

	changed := false
	for rel, data := range desired {
		dir := filepath.Join(root, rel)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return changed, fmt.Errorf("failed to create %s: %w", dir, err)
		}
		written, err := writeFileIfChanged(filepath.Join(dir, trustStoreCertFile), root, data)
		if err != nil {
			return changed, fmt.Errorf("failed to write trust store %s: %w", rel, err)
		}
		changed = changed || written
	}

	types, err := os.ReadDir(root)
	if err != nil {
		return changed, err
	}
	for _, t := range types {
		if !t.IsDir() {
			continue
		}
		stores, err := os.ReadDir(filepath.Join(root, t.Name()))
		if err != nil {
			return changed, err
		}
		for _, s := range stores {
			rel := filepath.Join(t.Name(), s.Name())
			if _, ok := desired[rel]; ok {
				continue
			}
			if err := os.RemoveAll(filepath.Join(root, rel)); err != nil {
				return changed, fmt.Errorf("failed to delete trust store %s: %w", rel, err)
			}
			changed = true
		}
	}
	return changed, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *TrustStoreReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&notationv1alpha1.TrustStore{}).
		Complete(r)
}
