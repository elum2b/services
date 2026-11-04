package admin

import "context"

func (a *Admin) Bootstrap(ctx context.Context) error {
	ctx, cancel := a.withContext(ctx)
	defer cancel()
	return a.repository.Bootstrap(ctx)
}
