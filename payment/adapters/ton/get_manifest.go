package ton

import (
	"context"

	"github.com/elum2b/services/payment/tonconnect"
)

// GetManifest returns the public TON Connect metadata for an enabled workspace wallet.
func (a *TON) GetManifest(
	ctx context.Context,
	workspaceID string,
) (tonconnect.Manifest, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	return a.repository.GetEnabledTONConnectManifest(mergedCtx, workspaceID)
}
