package main

import (
	"context"
	"fmt"
	"os"

	"github.com/blang/semver/v4"
	"github.com/operator-framework/deppy/api/v1alpha1"
	"github.com/operator-framework/deppy/internal/solver"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type TestVariable struct {
	identifier  solver.Identifier
	constraints []solver.Constraint
}

func (i TestVariable) Identifier() solver.Identifier {
	return i.identifier
}

func (i TestVariable) Constraints() []solver.Constraint {
	return i.constraints
}

func (i TestVariable) GoString() string {
	return fmt.Sprintf("%q", i.Identifier())
}

func variable(id solver.Identifier, constraints ...solver.Constraint) solver.Variable {
	return TestVariable{
		identifier:  id,
		constraints: constraints,
	}
}

func getClient() (client.Client, error) {
	cfg := ctrl.GetConfigOrDie()
	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		return nil, err
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		return nil, err
	}
	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return nil, err
	}
	return c, nil
}

func generateSolverVariable(c client.Client, input v1alpha1.Input, inputs []v1alpha1.Input) (solver.Variable, error) {
	variable := TestVariable{
		identifier: solver.IdentifierFromString(input.GetName()),
	}
	if len(input.Spec.Constraints) == 0 {
		return variable, nil
	}
	if len(input.Spec.Constraints) > 1 {
		return nil, fmt.Errorf("unsupported: cannot specify more than one constraint")
	}

	constraint := input.Spec.Constraints[0]
	if constraint.Type != "olm.packageVersion" {
		return nil, fmt.Errorf("unsupported constraint type %q", constraint.Type)
	}
	ref, err := newPackageRef(constraint)
	if err != nil {
		return nil, err
	}
	dependencies := []solver.Identifier{}

	for _, item := range inputs {
		for _, property := range item.Spec.Properties {
			if property.Type != "olm.PackageRef" {
				continue
			}
			packageRef := property.Value["package"]
			if packageRef != ref.Package {
				continue
			}
			version := property.Value["version"]
			if !inSemverRange(ref.Range, version) {
				logrus.Infof("version %v not in version range %v", version, ref.Range)
				continue
			}
			logrus.Infof("found version %v in version range %v", version, ref.Range)
			dependencies = append(dependencies, solver.IdentifierFromString(item.GetName()))
		}
	}
	variable.constraints = []solver.Constraint{solver.Mandatory(), solver.Dependency(dependencies...)}

	return variable, nil
}

func run() error {
	c, err := getClient()
	if err != nil {
		return err
	}

	inputs := &v1alpha1.InputList{}
	if err := c.List(context.Background(), inputs); err != nil {
		return err
	}
	if len(inputs.Items) == 0 {
		return fmt.Errorf("zero input resources on the cluster")
	}

	variables := make([]solver.Variable, 0)
	for _, input := range inputs.Items {
		variable, err := generateSolverVariable(c, input, inputs.Items)
		if err != nil {
			return err
		}
		variables = append(variables, variable)
	}
	for _, variable := range variables {
		logrus.Infof("variable: %+v", variable)
	}

	s, err := solver.New(solver.WithInput(variables))
	if err != nil {
		return err
	}
	installed, err := s.Solve(context.Background())
	if err != nil {
		return err
	}
	for _, install := range installed {
		fmt.Println(install)
	}

	return nil
}

func handleConstraint(constraint v1alpha1.Constraint) (*PackageRef, error) {
	if constraint.Type != "olm.packageVersion" {
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
	version, ok := constraint.Value["version"]
	if !ok {
		return nil, fmt.Errorf("invalid key for olm.packageVersion constraint type: missing version")
	}
	return &PackageRef{
		Package: packageRef,
		Range:   version,
	}, nil
}
