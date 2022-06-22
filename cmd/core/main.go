package main

import (
	"context"
	"fmt"
	"os"

	"github.com/operator-framework/deppy/api/v1alpha1"
	"github.com/operator-framework/deppy/cmd/util"
	"github.com/operator-framework/deppy/internal/sourcer"
	"github.com/sirupsen/logrus"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

var (
	packageSet = []string{"combo", "prometheus-operator"}
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
	// Create Input resources for the constraint-only Input resources
	// Source content from an external source
	// Filter out candidate content using filters
	// Ensure that filtered sources are created as property-only Input resources
	c, err := util.GetClient()
	if err != nil {
		return err
	}
	var (
		filters []sourcer.FilterFn
	)
	for _, name := range packageSet {
		if err := applyConstraintInput(ctx, c, name); err != nil {
			return fmt.Errorf("failed to generate a constraint input for the %s package: %v", name, err)
		}
		filters = append(filters, sourcer.WithPackageName(name))
	}

	candidates, err := sourcer.NewCatalogSourceHandler(c).Source(context.Background())
	if err != nil {
		return fmt.Errorf("failed to source candidates: %w", err)
	}
	sources := candidates.Filter(filters...)

	// TODO: run ths Apply package instead
	for _, bundle := range sources {
		logrus.Infof("generating an input for the %s bundle", bundle.String())
		if err := applyCandidateInput(ctx, c, bundle); err != nil {
			return err
		}
	}
	return nil
}

func generateInputPackageName(packageName string) string {
	return fmt.Sprintf("po-%s", packageName)
}

func generateInputCatalogName(packageName string) string {
	return fmt.Sprintf("catalog-%s", packageName)
}

func applyConstraintInput(ctx context.Context, c client.Client, packageName string) error {
	candidateInput := &v1alpha1.Input{}
	candidateInput.SetName(generateInputPackageName(packageName))

	// TODO: add a controller owner reference
	_, err := controllerutil.CreateOrUpdate(ctx, c, candidateInput, func() error {
		candidateInput.Spec = v1alpha1.InputSpec{
			InputClassName: "core",
			Constraints: []v1alpha1.Constraint{
				{
					Type: "olm.RequirePackage",
					Value: map[string]string{
						"package": packageName,
					},
				},
			},
		}
		return nil
	})
	return err
}

func applyCandidateInput(ctx context.Context, c client.Client, b sourcer.Bundle) error {
	candidateInput := &v1alpha1.Input{}
	candidateInput.SetName(generateInputCatalogName(b.Name))

	// parse the output of resolved identifiers; check whether an ID is prefixed with catalog-*,
	// and attempt to ensure it gets stamped out by a BI resource.
	// for this, we basically only need the bundle image, which we already have in the bundle structure,
	// and so we'd just need a way to map index ID name <-> bundle name here.
	// TODO: ID becomes a field on the Input resource vs. relying on the metadata.Name internally?

	// TODO: add a controller owner reference
	_, err := controllerutil.CreateOrUpdate(ctx, c, candidateInput, func() error {
		candidateInput.Spec = v1alpha1.InputSpec{
			InputClassName: "core",
			Properties: []v1alpha1.Property{
				{
					Type: "olm.packageVersion",
					Value: map[string]string{
						"package": b.PackageName,
						"version": b.Version,
					},
				},
			},
		}
		return nil
	})
	return err
}
