package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/operator-framework/deppy/internal/constraints"
	"github.com/operator-framework/deppy/internal/olm"
	olmsource "github.com/operator-framework/deppy/internal/olm/source"
	"github.com/operator-framework/deppy/internal/solver"
	"github.com/operator-framework/deppy/internal/source"
	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
	"gomodules.xyz/jsonpatch/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/yaml"
)

const (
	plainProvisionerID    = "core-rukpak-io-plain"
	registryProvisionerID = "core-rukpak-io-registry"
)

func main() {
	scheme := runtime.NewScheme()
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(rukpakv1alpha1.AddToScheme(scheme))

	kubeClient, err := client.New(ctrl.GetConfigOrDie(), client.Options{
		Scheme: scheme,
	})
	if err != nil {
		fmt.Println(err)
		return
	}

	// create deppy source and grab all the bundles
	ctx := context.Background()
	deppySource := olmsource.NewCatalogSourceDeppySource(kubeClient, "olm", "operatorhubio-catalog")
	if err := deppySource.Sync(ctx); err != nil {
		fmt.Println(err)
		return
	}

	// create a constraint builder (or variable builder...)
	constraintBuilder := constraints.NewConstraintBuilder(deppySource, olm.EntityToVariable,
		constraints.WithConstraintGenerators([]constraints.ConstraintGenerator{
			olm.RequirePackage("ack-sfn-controller", "", ""),
			olm.RequirePackage("kubestone", "", ""),
			olm.RequirePackage("cert-manager", "", ""),
			olm.RequirePackage("noobaa-operator", "", ""),
			olm.RequirePackage("kong", "", ""),
			olm.PackageUniqueness(),
			olm.GVKUniqueness(),
		}),
	)

	// imagine a nice interface to create the solver which takes the sources and constraint builder
	variables, err := constraintBuilder.Variables(ctx)
	if err != nil {
		fmt.Println(err)
		return
	}
	operatorSolver, err := solver.New(solver.WithInput(variables))
	if err != nil {
		fmt.Println(err)
		return
	}

	selection, err := operatorSolver.Solve(ctx)
	if err != nil {
		fmt.Println(err)
		return
	}
	var images []Bundle

	for _, item := range selection {
		id := item.Identifier()
		if !strings.Contains(id.String(), ":") {
			continue
		}

		entity := deppySource.Get(ctx, source.EntityID(id.String()))
		image, err := entity.GetProperty("olm.bundlePath")
		if err != nil {
			fmt.Println(err)
			return
		}
		name, err := entity.GetProperty("olm.packageName")
		if err != nil {
			fmt.Println(err)
			return
		}
		images = append(images, Bundle{
			name:  name,
			image: image,
		})
	}
	if err := generateResolveSet(ctx, kubeClient, images...); err != nil {
		fmt.Println(err)
		return
	}
}

type Bundle struct {
	name  string
	image string
}

// TODO: type ResolveSetGenerator interface {...}
// TODO: how to hook into the uploader service?
// TODO: what happens when a resolution is removed?

func generateResolveSet(ctx context.Context, c client.Client, images ...Bundle) error {
	var (
		output []string
	)
	// TODO: attach owner reference from parent
	resolveset := generateChildBundleDeployment(images...)
	for _, rs := range resolveset {
		yamlData, err := yaml.Marshal(rs)
		if err != nil {
			return err
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
		return err
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
	return err
}

func generateChildBundleDeployment(images ...Bundle) []*unstructured.Unstructured {
	// generate the child resolveset
	var res []*unstructured.Unstructured
	for _, image := range images {
		res = append(res, buildBundleDeployment(fmt.Sprintf("resolveset-%s", image.name), image.image))
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

type JsonPatchType struct {
	From client.Object
}

// Type is the PatchType of the patch.
func (j *JsonPatchType) Type() types.PatchType {
	return types.JSONPatchType
}

// Data is the raw data representing the patch.
func (j *JsonPatchType) Data(obj client.Object) ([]byte, error) {
	// ignore reversion
	if meta, ok := obj.(metav1.Object); ok {
		meta.SetResourceVersion("")
	}
	if meta, ok := j.From.(metav1.Object); ok {
		meta.SetResourceVersion("")
	}
	if kinded, ok := obj.(schema.ObjectKind); ok {
		kinded.SetGroupVersionKind(j.From.GetObjectKind().GroupVersionKind())
	}
	return CreatePatchContent(j.From, obj)
}

func CreatePatchContent(origin, modified interface{}) ([]byte, error) {
	o, e := json.Marshal(origin)
	if e != nil {
		return nil, e
	}
	m, e := json.Marshal(modified)
	if e != nil {
		return nil, e
	}
	patches, e := jsonpatch.CreatePatch(o, m)
	if e != nil {
		return nil, e
	}
	out, e := json.Marshal(patches)
	if e != nil {
		return nil, e
	}
	return out, nil
}
