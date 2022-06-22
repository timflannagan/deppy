package sourcer

import (
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
)

type filterSourceFn func(cs operatorsv1alpha1.CatalogSource) bool

type sources []operatorsv1alpha1.CatalogSource

func (s sources) Filter(f filterSourceFn) sources {
	var (
		filtered []operatorsv1alpha1.CatalogSource
	)
	for _, source := range s {
		if f(source) {
			filtered = append(filtered, source)
		}
	}
	return filtered
}

func byConnectionReadiness(cs operatorsv1alpha1.CatalogSource) bool {
	if cs.Status.GRPCConnectionState == nil {
		return false
	}
	if cs.Status.GRPCConnectionState.Address == "" {
		return false
	}
	return cs.Status.GRPCConnectionState.LastObservedState == "READY"
}

type Bundles []Bundle

func (bundles Bundles) Filter(filters ...FilterFn) []Bundle {
	var (
		filtered []Bundle
	)
	for _, bundle := range bundles {
		if !bundle.filter(filters...) {
			continue
		}
		filtered = append(filtered, bundle)
	}
	return filtered
}

func (b Bundle) filter(filters ...FilterFn) bool {
	for _, filter := range filters {
		if filter(b) {
			return true
		}
	}
	return false
}

// func (bundles Bundles) Latest() (*Bundle, error) {
// 	return bundles.Filter(byHighestSemver)
// }

// func byHighestSemver(currBundle, desiredBundle *Bundle) bool {
// 	currV, err := semver.Parse(currBundle.Version)
// 	if err != nil {
// 		return false
// 	}
// 	desiredV, err := semver.Parse(desiredBundle.Version)
// 	if err != nil {
// 		return false
// 	}
// 	return currV.Compare(desiredV) == 1
// }
