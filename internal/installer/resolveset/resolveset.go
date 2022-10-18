package resolveset

import (
	"context"
	"fmt"
	"strings"

	install "github.com/operator-framework/deppy/internal/installer"
	olmsource "github.com/operator-framework/deppy/internal/olm/source"
	"github.com/operator-framework/deppy/internal/solver"
	"github.com/operator-framework/deppy/internal/source"
	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/yaml"
)

// TODO: type ResolveSetGenerator interface {...}
// TODO: how to hook into the uploader service?
// TODO: what happens when a resolution is removed?

const (
	plainProvisionerID    = "core-rukpak-io-plain"
	registryProvisionerID = "core-rukpak-io-registry"
)

func New(c client.Client, source *olmsource.CatalogSourceDeppySource) install.Installer {
	return &resolveSet{
		Client: c,
		source: source,
	}
}

type resolveSet struct {
	client.Client
	source *olmsource.CatalogSourceDeppySource
}

type bundleMetadata struct {
	name  string
	image string
}

func (g *resolveSet) Install(ctx context.Context, variables ...solver.Variable) (bool, error) {
	metadata, err := generateBundleMetadata(ctx, g.source, variables...)
	if err != nil {
		return false, err
	}
	resolveset, err := generateResolveSet(ctx, g.Client, metadata...)
	if err != nil {
		return false, err
	}
	fmt.Println("generated the", resolveset.GetName(), "resolveset bundledeployment")

	return true, nil
}

func generateBundleMetadata(
	ctx context.Context,
	deppysource *olmsource.CatalogSourceDeppySource,
	variables ...solver.Variable,
) ([]bundleMetadata, error) {
	var (
		metadata []bundleMetadata
	)
	for _, item := range variables {
		id := item.Identifier()
		if !strings.Contains(id.String(), ":") {
			continue
		}

		// build the relevant metadata that the resolveset generation requires
		entity := deppysource.Get(ctx, source.EntityID(id.String()))
		image, err := entity.GetProperty("olm.bundlePath")
		if err != nil {
			return nil, err
		}
		name, err := entity.GetProperty("olm.packageName")
		if err != nil {
			return nil, err
		}
		metadata = append(metadata, bundleMetadata{
			name:  name,
			image: image,
		})
	}
	return metadata, nil
}

func generateResolveSet(ctx context.Context, c client.Client, metadata ...bundleMetadata) (client.Object, error) {
	// TODO: []func{ generatechild, generateCM, generateParent }?
	var (
		output []string
	)
	resolveset := generateChildBundleDeployment(metadata...)
	for _, rs := range resolveset {
		yamlData, err := yaml.Marshal(rs)
		if err != nil {
			return nil, err
		}
		output = append(output, string(yamlData))
	}

	cm := &corev1.ConfigMap{}
	cm.SetName("test")
	cm.SetNamespace("rukpak-system")

	if _, err := controllerutil.CreateOrUpdate(ctx, c, cm, func() error {
		if len(cm.Data) == 0 {
			cm.Data = make(map[string]string)
		}
		cm.Data["manifests.yaml"] = string(strings.Join(output, "---\n"))
		return nil
	}); err != nil {
		return nil, err
	}

	bd := &rukpakv1alpha1.BundleDeployment{}
	bd.SetName("resolveset-parent")

	_, err := controllerutil.CreateOrUpdate(ctx, c, bd, func() error {
		bd.Spec = rukpakv1alpha1.BundleDeploymentSpec{
			ProvisionerClassName: plainProvisionerID,
			Template: &rukpakv1alpha1.BundleTemplate{
				Spec: rukpakv1alpha1.BundleSpec{
					ProvisionerClassName: plainProvisionerID,
					Source: rukpakv1alpha1.BundleSource{
						Type: rukpakv1alpha1.SourceTypeLocal,
						Local: &rukpakv1alpha1.LocalSource{
							ConfigMapRef: &rukpakv1alpha1.ConfigMapRef{
								Name:      cm.GetName(),
								Namespace: cm.GetNamespace(),
							},
						},
					},
				},
			},
		}

		return nil
	})
	return bd, err
}

func generateChildBundleDeployment(metadata ...bundleMetadata) []*unstructured.Unstructured {
	var res []*unstructured.Unstructured
	for _, bundle := range metadata {
		bd := buildBundleDeployment(fmt.Sprintf("resolveset-%s", bundle.name), bundle.image)
		res = append(res, bd)
	}
	return res
}

// buildBundleDeployment is responsible for taking a name and image to create an embedded BundleDeployment
func buildBundleDeployment(name, image string) *unstructured.Unstructured {
	// We use unstructured here to avoid problems of serializing default values when sending patches to the apiserver.
	// If you use a typed object, any default values from that struct get serialized into the JSON patch, which could
	// cause unrelated fields to be patched back to the default value even though that isn't the intention. Using an
	// unstructured ensures that the patch contains only what is specified. Using unstructured like this is basically
	// identical to "kubectl apply -f"
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": rukpakv1alpha1.GroupVersion.String(),
		"kind":       rukpakv1alpha1.BundleDeploymentKind,
		"metadata": map[string]interface{}{
			"name": name,
		},
		"spec": map[string]interface{}{
			"provisionerClassName": "core-rukpak-io-plain",
			"template": map[string]interface{}{
				"metadata": map[string]interface{}{
					"labels": nil,
				},
				"spec": map[string]interface{}{
					"provisionerClassName": "core-rukpak-io-registry",
					"source": map[string]interface{}{
						"type": rukpakv1alpha1.SourceTypeImage,
						"image": rukpakv1alpha1.ImageSource{
							Ref: image,
						},
					},
				},
			},
		},
	}}
}
