package contextutil

import "context"

func Normalize(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func Merge(rootCtx context.Context, requestCtx context.Context) (context.Context, context.CancelFunc) {
	rootCtx = Normalize(rootCtx)
	if requestCtx == nil {
		return context.WithCancel(rootCtx)
	}

	ctx, cancel := context.WithCancel(requestCtx)
	stop := context.AfterFunc(rootCtx, cancel)
	return ctx, func() {
		stop()
		cancel()
	}
}
