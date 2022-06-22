package applier

import (
	"context"

	"github.com/operator-framework/deppy/internal/sourcer"
)

type Applier interface {
	Apply(context.Context, *sourcer.Bundle) error
}
