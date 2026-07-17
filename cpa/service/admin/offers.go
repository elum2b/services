package admin

import (
	"context"
	"time"

	json "github.com/goccy/go-json"

	"github.com/elum2b/services/cpa/repository"
)

type UpsertOfferParams struct {
	WorkspaceID       string
	ID                string
	Payload           json.RawMessage
	Target            json.RawMessage
	CodeMode          string
	CodeSource        *string
	SharedCode        *string
	GeneratedLength   *int16
	GeneratedAlphabet *string
	IsActive          bool
	StartAt           *time.Time
	EndAt             *time.Time
}

func (a *Admin) UpsertOffer(ctx context.Context, params UpsertOfferParams) error {

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	return a.repository.UpsertOffer(mergedCtx, repository.UpsertOfferParams(params))

}

func (a *Admin) GetOffer(ctx context.Context, workspaceID, cpaID string) (OfferModel, error) {

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	bundle, err := a.repository.GetOfferBundle(mergedCtx, workspaceID, cpaID)
	if err != nil {
		return OfferModel{}, err
	}

	return mapOffer(bundle.Offer, bundle.Localizations, bundle.Rewards), nil

}

func (a *Admin) ListOffers(ctx context.Context, workspaceID string, page Page) ([]OfferModel, error) {

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	limit, offset := normalizePage(page)
	bundles, err := a.repository.ListOfferBundles(mergedCtx, workspaceID, limit, offset)
	if err != nil {
		return nil, err
	}

	result := make([]OfferModel, 0, len(bundles))
	for _, bundle := range bundles {
		result = append(result, mapOffer(bundle.Offer, bundle.Localizations, bundle.Rewards))
	}

	return result, nil

}

func (a *Admin) DeleteOffer(ctx context.Context, workspaceID, cpaID string) (int64, error) {

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	return a.repository.DeleteOffer(mergedCtx, workspaceID, cpaID)

}
