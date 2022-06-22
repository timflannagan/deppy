package applier

import (
	"context"

	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/operator-framework/deppy/internal/sourcer"
)

const (
	plainProvisionerID    = "core.rukpak.io/plain"
	registryProvisionerID = "core.rukpak.io/registry"
)

type biApplier struct {
	client.Client
}

type inputApplier struct {
	client.Client
}

func NewBundleInstanceHandler(c client.Client) Applier {
	return &biApplier{
		Client: c,
	}
}

func NewInputApplier(c client.Client) Applier {
	return &inputApplier{
		Client: c,
	}
}

func (a *inputApplier) Apply(ctx context.Context, b *sourcer.Bundle) error {
	bi := &rukpakv1alpha1.BundleInstance{}
	bi.SetName("stub")
	// controllerRef := metav1.NewControllerRef(po, po.GroupVersionKind())

	_, err := controllerutil.CreateOrUpdate(ctx, a.Client, bi, func() error {
		// bi.SetOwnerReferences([]metav1.OwnerReference{*controllerRef})
		bi.Spec = *buildBundleInstance(b.Image)
		return nil
	})
	return err
}

func (a *biApplier) Apply(ctx context.Context, b *sourcer.Bundle) error {
	bi := &rukpakv1alpha1.BundleInstance{}
	bi.SetName("stub")
	// controllerRef := metav1.NewControllerRef(po, po.GroupVersionKind())

	_, err := controllerutil.CreateOrUpdate(ctx, a.Client, bi, func() error {
		// bi.SetOwnerReferences([]metav1.OwnerReference{*controllerRef})
		bi.Spec = *buildBundleInstance(b.Image)
		return nil
	})
	return err
}

// buildBundleInstance is responsible for taking a name and image to create an embedded BundleInstance
func buildBundleInstance(image string) *rukpakv1alpha1.BundleInstanceSpec {
	return &rukpakv1alpha1.BundleInstanceSpec{
		ProvisionerClassName: plainProvisionerID,
		// TODO(tflannag): Investigate why the metadata key is empty when this
		// resource has been created on cluster despite the field being omitempty.
		Template: &rukpakv1alpha1.BundleTemplate{
			Spec: rukpakv1alpha1.BundleSpec{
				// TODO(tflannag): Dynamically determine provisioner ID based on bundle
				// format? Do we need an API for discovering available provisioner IDs
				// in the cluster, and to map those ID(s) to bundle formats?
				ProvisionerClassName: registryProvisionerID,
				Source: rukpakv1alpha1.BundleSource{
					Type: rukpakv1alpha1.SourceTypeImage,
					Image: &rukpakv1alpha1.ImageSource{
						Ref: image,
					},
				},
			},
		},
	}
}
