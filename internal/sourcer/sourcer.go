package sourcer

import (
	"context"
	"fmt"

	"github.com/operator-framework/deppy/api/v1alpha1"
)

type Bundle struct {
	Name        string
	PackageName string
	ChannelName string
	Version     string
	Image       string
	Replaces    string
	Skips       []string
	Properties  []v1alpha1.Property
	SourceName  string
}

func (b Bundle) String() string {
	return fmt.Sprintf("Name: %s; Package: %s; Channel: %s; Version: %s; Image: %s; Replaces: %s", b.Name, b.PackageName, b.ChannelName, b.Version, b.Image, b.Replaces)
}

type Sourcer interface {
	Source(context.Context, ...FilterFn) (Bundles, error)
}
