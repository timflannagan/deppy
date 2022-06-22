package controllers

import (
	"context"
	"sort"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logr "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/operator-framework/deppy/api/v1alpha1"
	platformv1alpha1 "github.com/operator-framework/deppy/api/v1alpha1"
)

// PlatformOperatorReconciler reconciles a PlatformOperator object
type PlatformOperatorReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

const (
	packageConstraintType = "olm.RequirePackage"
	packageValueKey       = "package"
	platformResolution    = "platform"
	platformSingletonName = "cluster"
)

//+kubebuilder:rbac:groups=core.deppy.io,resources=platformoperators,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core.deppy.io,resources=platformoperators/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=core.deppy.io,resources=platformoperators/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.11.2/pkg/reconcile
func (r *PlatformOperatorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logr.FromContext(ctx)
	log.Info("reconciling request", "req", req.NamespacedName)
	defer log.Info("finished reconciling request", "req", req.NamespacedName)

	// TODO: flesh out status condition management
	po := &platformv1alpha1.PlatformOperator{}
	if err := r.Get(ctx, req.NamespacedName, po); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	// filter out any non-cluster singletons
	if po.GetName() != platformSingletonName {
		return ctrl.Result{}, nil
	}
	defer func() {
		po := po.DeepCopy()
		po.ObjectMeta.ManagedFields = nil
		if err := r.Status().Patch(ctx, po, client.Apply, client.FieldOwner("platformoperator")); err != nil {
			log.Error(err, "failed to patch status")
		}
	}()

	res := &v1alpha1.Resolution{}
	if err := r.Get(ctx, types.NamespacedName{Name: platformResolution}, res); err != nil {
		log.Error(err, "failed to find the resolution resource")
		return ctrl.Result{}, err
	}

	var (
		desiredPackages []v1alpha1.Constraint
	)
	for _, name := range po.Spec.Packages {
		desiredPackages = append(desiredPackages, newPackageRequirement(name))
	}
	res.Spec.Constraints = desiredPackages
	sort.SliceStable(res.Spec.Constraints, func(i, j int) bool {
		x, ok := res.Spec.Constraints[i].Value[packageValueKey]
		if !ok {
			return true
		}
		y, ok := res.Spec.Constraints[j].Value[packageValueKey]
		if !ok {
			return true
		}
		return x < y
	})
	return ctrl.Result{}, r.Client.Update(ctx, res)
}

func newPackageRequirement(packageName string) v1alpha1.Constraint {
	return v1alpha1.Constraint{
		Type: packageConstraintType,
		Value: map[string]string{
			packageValueKey: packageName,
		},
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *PlatformOperatorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.PlatformOperator{}).
		Watches(&source.Kind{Type: &v1alpha1.Resolution{}}, handler.EnqueueRequestsFromMapFunc(func(o client.Object) []reconcile.Request {
			resolution := o.(*v1alpha1.Resolution)
			if resolution.GetName() == platformResolution {
				return []reconcile.Request{}
			}
			return nil
		})).
		Complete(r)
}
