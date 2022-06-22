package sourcer

import (
	"context"
	"errors"
	"fmt"

	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/operator-framework/operator-registry/pkg/api"
	registryClient "github.com/operator-framework/operator-registry/pkg/client"
	"github.com/sirupsen/logrus"
	utilerror "k8s.io/apimachinery/pkg/util/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	channelName = "4.12"
)

var (
	ErrNoCandidates = errors.New("failed to find any candidate bundles")
)

type catalogSource struct {
	client.Client
}

func NewCatalogSourceHandler(c client.Client) Sourcer {
	return &catalogSource{
		Client: c,
	}
}

func (cs catalogSource) Source(ctx context.Context, filters ...FilterFn) (Bundles, error) {
	css := &operatorsv1alpha1.CatalogSourceList{}
	if err := cs.List(ctx, css); err != nil {
		return nil, err
	}
	if len(css.Items) == 0 {
		return nil, fmt.Errorf("failed to query for any catalog sources in the cluster")
	}
	sources := sources(css.Items)

	candidates, err := sources.Filter(byConnectionReadiness).GetCandidates(ctx, filters...)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, ErrNoCandidates
	}
	return candidates, nil
}

type FilterFn func(interface{}) bool

func WithPackageName(name string) FilterFn {
	return func(obj interface{}) bool {
		switch v := obj.(type) {
		case *apiWrapper:
			b := obj.(*apiWrapper)
			return b.GetPackageName() == name
		case Bundle:
			b := obj.(Bundle)
			return b.PackageName == name
		default:
			logrus.Infof("unknown type %T", v)
			return false
		}
	}
}

func WithChannelName(name string) FilterFn {
	return func(obj interface{}) bool {
		switch v := obj.(type) {
		case *apiWrapper:
			b := obj.(*apiWrapper)
			return b.GetChannelName() == name
		case Bundle:
			b := obj.(Bundle)
			return b.ChannelName == name
		default:
			logrus.Infof("unknown type %q", v)
		}
		return true
	}
}

func And(filters ...FilterFn) FilterFn {
	return func(obj interface{}) bool {
		for _, f := range filters {
			if !f(obj) {
				return false
			}
		}
		return true
	}
}

type apiWrapper struct {
	*api.Bundle
}

func newWrapper(b *api.Bundle) *apiWrapper {
	return &apiWrapper{Bundle: b}
}

func (b *apiWrapper) Filter(filters ...FilterFn) bool {
	for _, filter := range filters {
		if !filter(b) {
			return true
		}
	}
	return true
}

func (s sources) GetCandidates(ctx context.Context, filters ...FilterFn) (Bundles, error) {
	var (
		errors     []error
		candidates Bundles
	)
	// TODO: Should build a cache for efficiency
	for _, cs := range s {
		// Note(tflannag): Need to account for grpc-based CatalogSource(s) that
		// specify a spec.Address or a spec.Image, so ensure this field exists, and
		// it's not empty before creating a registry client.
		rc, err := registryClient.NewClient("localhost:50051")
		if err != nil {
			errors = append(errors, fmt.Errorf("failed to register client from the %s/%s grpc connection: %w", cs.GetName(), cs.GetNamespace(), err))
			continue
		}
		it, err := rc.ListBundles(ctx)
		if err != nil {
			errors = append(errors, fmt.Errorf("failed to list bundles from the %s/%s catalog: %w", cs.GetName(), cs.GetNamespace(), err))
			continue
		}
		for b := it.Next(); b != nil; b = it.Next() {
			if !newWrapper(b).Filter(filters...) {
				continue
			}
			candidates = append(candidates, Bundle{
				Name:        b.GetCsvName(),
				PackageName: b.GetPackageName(),
				ChannelName: b.GetChannelName(),
				Version:     b.GetVersion(),
				Image:       b.GetBundlePath(),
				Skips:       b.GetSkips(),
				Replaces:    b.GetReplaces(),
			})
		}
	}
	if len(errors) != 0 {
		return nil, utilerror.NewAggregate(errors)
	}
	return candidates, nil
}
