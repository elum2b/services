package payment

import (
	"context"

	"github.com/elum2b/services/internal/utils/contextutil"
)

func normalizeLifecycleContext(ctx context.Context) context.Context {
	return contextutil.Normalize(ctx)
}

func mergeContexts(lifecycleCtx context.Context, methodCtx context.Context) (context.Context, context.CancelFunc) {
	return contextutil.Merge(lifecycleCtx, methodCtx)
}
