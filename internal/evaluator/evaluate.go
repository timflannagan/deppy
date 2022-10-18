package evaluator

import (
	"context"

	"github.com/operator-framework/deppy/internal/constraints"
	"github.com/operator-framework/deppy/internal/installer/resolveset"
	"github.com/operator-framework/deppy/internal/olm"
	"github.com/operator-framework/deppy/internal/olm/source"
	"github.com/operator-framework/deppy/internal/solver"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type evaluator struct {
	client.Client
}

func New(c client.Client) *evaluator {
	return &evaluator{
		Client: c,
	}
}

func (e *evaluator) Evaluate(ctx context.Context) error {
	// TODO: refactor to builder pattern?
	deppySource := source.NewCatalogSourceDeppySource(e.Client, "olm", "operatorhubio-catalog")
	if err := deppySource.Sync(ctx); err != nil {
		return err
	}
	installer := resolveset.New(e.Client, deppySource)

	// create a constraint builder (or variable builder...)
	constraintBuilder := constraints.NewConstraintBuilder(deppySource, olm.EntityToVariable,
		constraints.WithConstraintGenerators([]constraints.ConstraintGenerator{
			olm.RequirePackage("ack-sfn-controller", "", ""),
			olm.RequirePackage("kubestone", "", ""),
			olm.RequirePackage("cert-manager", "", ""),
			olm.RequirePackage("noobaa-operator", "", ""),
			olm.RequirePackage("kong", "", ""),
			olm.RequirePackage("datadog-operator", "", ""),
			olm.PackageUniqueness(),
			olm.GVKUniqueness(),
		}),
	)

	// imagine a nice interface to create the solver which takes the sources and constraint builder
	variables, err := constraintBuilder.Variables(ctx)
	if err != nil {
		return err
	}
	operatorSolver, err := solver.New(solver.WithInput(variables))
	if err != nil {
		return err
	}
	selection, err := operatorSolver.Solve(ctx)
	if err != nil {
		return err
	}
	_, err = installer.Install(ctx, selection...)
	if err != nil {
		return err
	}

	return nil
}
