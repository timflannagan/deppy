package install

import (
	"context"

	"github.com/operator-framework/deppy/internal/solver"
)

// TODO: type ResolveSetGenerator interface {...}
// TODO: how to hook into the uploader service?
// TODO: what happens when a resolution is removed?

type Installer interface {
	Install(context.Context, ...solver.Variable) (bool, error)
}
