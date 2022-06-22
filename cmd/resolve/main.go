package main

import (
	"context"
	"fmt"
	"os"
	"sort"

	"github.com/blang/semver/v4"
	"github.com/operator-framework/deppy/api/v1alpha1"
	"github.com/operator-framework/deppy/cmd/util"
	"github.com/operator-framework/deppy/internal/solver"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	c, err := util.GetClient()
	if err != nil {
		return fmt.Errorf("failed to get client: %v", err)
	}
	inputs := &v1alpha1.InputList{}
	if err := c.List(ctx, inputs); err != nil {
		return err
	}

	// TODO: account for existing resources as well
	variables := make([]solver.Variable, 0)
	for _, input := range inputs.Items {
		variable, err := generateSolverVariable(c, input, inputs.Items)
		if err != nil {
			return err
		}
		if variable == nil {
			// TODO: this shouldn't happen but catch this anyways
			return fmt.Errorf("invalid variable returned")
		}
		variables = append(variables, variable)
	}
	s, err := solver.New(solver.WithInput(variables))
	if err != nil {
		return err
	}
	installed, err := s.Solve(context.Background())
	if err != nil {
		return err
	}
	if len(installed) == 0 {
		return fmt.Errorf("failed to generate a resolution - empty resolution output")
	}
	sort.SliceStable(installed, func(i, j int) bool {
		return installed[i].Identifier() < installed[j].Identifier()
	})

	var (
		installedIDs []string
	)
	for _, install := range installed {
		installedIDs = append(installedIDs, install.Identifier().String())
	}
	for _, input := range inputs.Items {
		input.Status.IDs = installedIDs
		meta.SetStatusCondition(&input.Status.Conditions, metav1.Condition{
			Type:    "Resolved",
			Status:  metav1.ConditionTrue,
			Reason:  "SuccessfulResolution",
			Message: "Successfully resolved solver inputs",
		})

		i := input.DeepCopy()
		i.ObjectMeta.ManagedFields = nil
		if err := c.Status().Patch(ctx, i, client.Apply, client.FieldOwner("problem")); err != nil {
			return err
		}
	}

	// TODO: Input here either needs to be a singleton, or we have a higher-level API that handles this for us.
	// - As a client, I just want to interface with a single API that contains the list of IDs I need to do $something with.
	// - Internally, that controller can be listing the Inputs in the cluster, performing resolution, and bubbling up that list to a single API.
	// - And that API could be as simple as a Resolution singleton.

	return nil
}

func generateSolverVariable(c client.Client, input v1alpha1.Input, inputs []v1alpha1.Input) (solver.Variable, error) {
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
