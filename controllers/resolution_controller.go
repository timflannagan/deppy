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
	"sort"

	"github.com/blang/semver/v4"
	"github.com/operator-framework/deppy/api/v1alpha1"
	"github.com/operator-framework/deppy/internal/solver"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// ResolutionReconciler reconciles a Resolution object
type ResolutionReconciler struct {
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
func (r *ResolutionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)

	res := &v1alpha1.Resolution{}
	if err := r.Get(ctx, req.NamespacedName, res); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	defer func() {
		res := res.DeepCopy()
		res.ObjectMeta.ManagedFields = nil
		if err := r.Status().Patch(ctx, res, client.Apply, client.FieldOwner("resolutions")); err != nil {
			l.Error(err, "failed to patch status")
		}
	}()
	res.Status.IDs = nil

	inputs := &v1alpha1.InputList{}
	if err := r.List(context.Background(), inputs); err != nil {
		return ctrl.Result{}, err
	}
	if len(inputs.Items) == 0 {
		meta.SetStatusCondition(&res.Status.Conditions, metav1.Condition{
			Type:    "Resolved",
			Status:  metav1.ConditionFalse,
			Reason:  "NoRuntimeInputs",
			Message: "Waiting for runtime Input resources to be defined before performing resolution",
		})
		return ctrl.Result{}, nil
	}

	variables, err := r.EvaluateConstraints(res, inputs.Items)
	if err != nil {
		meta.SetStatusCondition(&res.Status.Conditions, metav1.Condition{
			Type:    "Resolved",
			Status:  metav1.ConditionFalse,
			Reason:  "ConstraintEvaluatorFailed",
			Message: err.Error(),
		})
		return ctrl.Result{}, err
	}
	if len(variables) == 0 {
		meta.SetStatusCondition(&res.Status.Conditions, metav1.Condition{
			Type:    "Resolved",
			Status:  metav1.ConditionFalse,
			Reason:  "ConstraintEvaluatorFailed",
			Message: "Failed to generate any internal solver.Variables",
		})
		return ctrl.Result{}, nil
	}

	s, err := solver.New(solver.WithInput(variables))
	if err != nil {
		meta.SetStatusCondition(&res.Status.Conditions, metav1.Condition{
			Type:    "Resolved",
			Status:  metav1.ConditionFalse,
			Reason:  "SolverInitializationFailed",
			Message: err.Error(),
		})
		return ctrl.Result{}, err
	}
	installed, err := s.Solve(context.Background())
	if err != nil {
		meta.SetStatusCondition(&res.Status.Conditions, metav1.Condition{
			Type:    "Resolved",
			Status:  metav1.ConditionFalse,
			Reason:  "SolverProblemFailed",
			Message: err.Error(),
		})
		return ctrl.Result{}, err
	}
	meta.SetStatusCondition(&res.Status.Conditions, metav1.Condition{
		Type:    "Resolved",
		Status:  metav1.ConditionTrue,
		Reason:  "SuccessfulResolution",
		Message: "Successfully resolved runtime input resources",
	})

	sort.SliceStable(installed, func(i, j int) bool {
		return installed[i].Identifier() < installed[j].Identifier()
	})

	res.Status.IDs = []string{}
	for _, install := range installed {
		res.Status.IDs = append(res.Status.IDs, install.Identifier().String())
	}

	return ctrl.Result{}, nil
}

func (r *ResolutionReconciler) EvaluateConstraints(res *v1alpha1.Resolution, inputs []v1alpha1.Input) ([]solver.Variable, error) {
	var (
		inputVars []solver.Variable
	)
	// Question: are we implicitly chaining together "AND" constraints here?
	for _, constraint := range res.Spec.Constraints {
		// TODO: avoid hardcoding this logic
		if constraint.Type != "olm.RequirePackage" {
			return nil, fmt.Errorf("unsupported constraint type %q", constraint.Type)
		}
		packageRef, ok := constraint.Value["package"]
		if !ok {
			return nil, fmt.Errorf("invalid key for olm.packageVersion constraint type: missing package")
		}
		// for each input: generate a solver.Variable even if it doesn't match any of the properties we're searching for right now
		for _, input := range inputs {
			variable := solver.GenericVariable{
				ID: solver.IdentifierFromString(input.GetName()),
			}
			dependencies := []solver.Identifier{}
			for _, property := range input.Spec.Properties {
				if property.Type != "olm.packageVersion" {
					continue
				}
				ref, ok := property.Value["package"]
				if !ok {
					return nil, fmt.Errorf("invalid key for olm.packageVersion property: missing package key value")
				}
				if packageRef != ref {
					continue
				}
				dependencies = append(dependencies, solver.IdentifierFromString(input.GetName()))
			}
			inputVars = append(inputVars, variable)
		}
	}

	return nil, nil
}

// func (r *ResolutionReconciler) CalculateInputVariables(res v1alpha1.Resolution, inputs []v1alpha1.Input) ([]solver.Variable, error) {
// 	// TODO: account for existing resources as well
// 	variables := make([]solver.Variable, 0)
// 	for _, input := range inputs {
// 		variable, err := r.calculateInputVariable(input, inputs)
// 		if err != nil {
// 			return nil, err
// 		}
// 		if variable == nil {
// 			return nil, fmt.Errorf("invalid variable returned")
// 		}
// 		variables = append(variables, variable)
// 	}
// 	return variables, nil
// }

func (r *ResolutionReconciler) evaluateResolutionConstraints(input v1alpha1.Input, inputs []v1alpha1.Input) (solver.Variable, error) {
	variable := solver.GenericVariable{
		ID: solver.IdentifierFromString(input.GetName()),
	}
	if len(input.Spec.Constraints) == 0 {
		return variable, nil
	}
	if len(input.Spec.Constraints) > 1 {
		return nil, fmt.Errorf("unsupported: cannot specify more than one constraint")
	}
	// TODO: support multiple constraint definitions
	// TODO: avoid hardcoding the supported constraint definition
	constraint := input.Spec.Constraints[0]
	if constraint.Type != "olm.RequirePackage" {
		return nil, fmt.Errorf("unsupported constraint type %q", constraint.Type)
	}
	ref, err := newPackageRef(constraint)
	if err != nil {
		return nil, err
	}
	dependencies := []solver.Identifier{}

	// Build up a set of dependent Inputs that match the requisite label
	// TODO: avoid hardcoding the property type.
	for _, item := range inputs {
		for _, property := range item.Spec.Properties {
			if property.Type != "olm.packageVersion" {
				continue
			}
			packageRef := property.Value["package"]
			if packageRef != ref.Package {
				continue
			}
			// version := property.Value["version"]
			// if !inSemverRange(ref.Range, version) {
			// 	logrus.Infof("version %v not in version range %v", version, ref.Range)
			// 	continue
			// }
			// logrus.Infof("found version %v in version range %v", version, ref.Range)
			dependencies = append(dependencies, solver.IdentifierFromString(item.GetName()))
		}
	}
	variable.Rules = []solver.Constraint{solver.Mandatory(), solver.Dependency(dependencies...)}
	return variable, nil
}

func handleConstraint(constraint v1alpha1.Constraint) (*PackageRef, error) {
	if constraint.Type != "olm.RequirePackage" {
		return nil, nil
	}
	return newPackageRef(constraint)
}

func inSemverRange(versionRange string, version string) bool {
	r, err := semver.ParseRange(versionRange)
	if err != nil {
		panic(err)
	}
	v, err := semver.Parse(version)
	if err != nil {
		panic(err)
	}
	return r(v)
}

type PackageRef struct {
	Package string
	Range   string
}

func newPackageRef(constraint v1alpha1.Constraint) (*PackageRef, error) {
	// verify there's a package key and a verison key
	packageRef, ok := constraint.Value["package"]
	if !ok {
		return nil, fmt.Errorf("invalid key for olm.packageVersion constraint type: missing package")
	}
	// version, ok := constraint.Value["version"]
	// if !ok {
	// 	return nil, fmt.Errorf("invalid key for olm.packageVersion constraint type: missing version")
	// }
	return &PackageRef{
		Package: packageRef,
		// Range:   version,
	}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ResolutionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.Resolution{}).
		Watches(&source.Kind{Type: &v1alpha1.Input{}}, handler.EnqueueRequestsFromMapFunc(func(o client.Object) []reconcile.Request {
			inputs := &v1alpha1.InputList{}
			if err := r.List(context.Background(), inputs); err != nil {
				return nil
			}
			res := make([]reconcile.Request, 0, len(inputs.Items))
			for _, input := range inputs.Items {
				res = append(res, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&input)})
			}
			return res
		})).
		Complete(r)
}
