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
// Modified by krax1337, 2026: return the manager and change channel by value,
// dropped the unused webhook server on :9443 (the TLS API server owns that
// port) and the client auth plugins import.

// Package kubenotation runs the controllers that materialise TrustPolicy and
// TrustStore custom resources into the local notation configuration directory.
package kubenotation

import (
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	notationv1alpha1 "github.com/krax1337/notation-aws-verifier/internal/kubenotation/api/v1alpha1"
	"github.com/krax1337/notation-aws-verifier/internal/kubenotation/controller"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(notationv1alpha1.AddToScheme(scheme))
}

type SetupResult struct {
	// CRDManager runs the TrustPolicy/TrustStore controllers, metrics and health probes.
	CRDManager manager.Manager
	// CRDChangeInformer receives a notification whenever trust policies or trust stores on disk change.
	CRDChangeInformer <-chan struct{}
}

func Setup(logger logr.Logger, metricsAddr string, probeAddr string, enableLeaderElection bool) (*SetupResult, error) {
	ctrl.SetLogger(logger)
	logger = logger.WithName("setup")

	cfg, err := ctrl.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("unable to get kubeconfig: %w", err)
	}

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme,
		Metrics: server.Options{
			BindAddress: metricsAddr,
		},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		// Kept from upstream so existing leases keep working after migration.
		LeaderElectionID: "d94abea7.nirmata.io",
	})
	if err != nil {
		logger.Error(err, "unable to create manager")
		return nil, fmt.Errorf("unable to create manager: %w", err)
	}

	changes := make(chan struct{}, 1)
	if err = (&controller.TrustPolicyReconciler{
		Client:            mgr.GetClient(),
		Scheme:            mgr.GetScheme(),
		CRDChangeInformer: changes,
	}).SetupWithManager(mgr); err != nil {
		logger.Error(err, "unable to create controller", "controller", "TrustPolicy")
		return nil, fmt.Errorf("unable to create controller TrustPolicy: %w", err)
	}
	if err = (&controller.TrustStoreReconciler{
		Client:            mgr.GetClient(),
		Scheme:            mgr.GetScheme(),
		CRDChangeInformer: changes,
	}).SetupWithManager(mgr); err != nil {
		logger.Error(err, "unable to create controller", "controller", "TrustStore")
		return nil, fmt.Errorf("unable to create controller TrustStore: %w", err)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		logger.Error(err, "unable to set up health check")
		return nil, fmt.Errorf("unable to set up health check: %w", err)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		logger.Error(err, "unable to set up ready check")
		return nil, fmt.Errorf("unable to set up ready check: %w", err)
	}

	return &SetupResult{
		CRDManager:        mgr,
		CRDChangeInformer: changes,
	}, nil
}
