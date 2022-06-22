/*
Copyright 2022.

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

package controllers

import (
	"context"
	"fmt"

	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/operator-framework/deppy/api/v1alpha1"
	"github.com/operator-framework/deppy/internal/sourcer"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// CatalogSourceAdapter reconciles a Resolution object
type CatalogSourceAdapter struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=core.deppy.io,resources=resolutions,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core.deppy.io,resources=resolutions/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=core.deppy.io,resources=resolutions/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.11.2/pkg/reconcile
func (r *CatalogSourceAdapter) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)
	l.Info("reconciling request", "request", req.String())
	defer l.Info("finished reconcile request")

	// TODO(tflannag): Do we need client-side filter here? How can we avoid creating hundreds of
	// Input resources where deppy has to list all of those resources?
	// We could also just periodically stream Inputs via a grpc client connection e.g. ServiceMonitors
	// or Prometheus scrape points like Joe had mentioned.
	catalogs := &operatorsv1alpha1.CatalogSourceList{}
	if err := r.List(ctx, catalogs); err != nil {
		return ctrl.Result{}, err
	}
	candidates, err := sourcer.NewCatalogSourceHandler(r.Client).Source(context.Background())
	if err != nil {
		return ctrl.Result{}, err
	}
	for _, candidate := range candidates {
		if err := r.applyCandidateInput(ctx, candidate); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

func generateInputCatalogName(packageName string) string {
	return fmt.Sprintf("catalog-%s", packageName)
}

func (r *CatalogSourceAdapter) applyCandidateInput(ctx context.Context, b sourcer.Bundle) error {
	candidateInput := &v1alpha1.Input{}
	candidateInput.SetName(generateInputCatalogName(b.Name))

	// parse the output of resolved identifiers; check whether an ID is prefixed with catalog-*,
	// and attempt to ensure it gets stamped out by a BI resource.
	// for this, we basically only need the bundle image, which we already have in the bundle structure,
	// and so we'd just need a way to map index ID name <-> bundle name here.
	// TODO: ID becomes a field on the Input resource vs. relying on the metadata.Name internally?

	// TODO: add a controller owner reference?
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, candidateInput, func() error {
		candidateInput.Spec = v1alpha1.InputSpec{
			InputClassName: "core",
			Properties:     b.Properties,
		}
		return nil
	})
	return err
}

// SetupWithManager sets up the controller with the Manager.
func (r *CatalogSourceAdapter) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorsv1alpha1.CatalogSource{}).
		Complete(r)
}
