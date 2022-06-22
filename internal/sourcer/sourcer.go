package sourcer

import (
	"context"
	"fmt"
)

type Bundle struct {
	Name        string
	PackageName string
	ChannelName string
	Version     string
	Image       string
	Replaces    string
	Skips       []string
}

func (b Bundle) String() string {
	return fmt.Sprintf("Name: %s; Package: %s; Channel: %s; Version: %s; Image: %s; Replaces: %s", b.Name, b.PackageName, b.ChannelName, b.Version, b.Image, b.Replaces)
}

type Sourcer interface {
	Source(context.Context, ...FilterFn) (Bundles, error)
}
