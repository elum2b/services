package admin

import (
	services "github.com/elum2b/services"
	"github.com/elum2b/services/payment/repository"
)

func normalizePage(params PageParams) (int32, int32) {
	limit := params.Limit
	if limit <= 0 {
		limit = 100
	}

	if limit > 1000 {
		limit = 1000
	}

	offset := params.Offset
	if offset < 0 {
		offset = 0
	}

	return limit, offset
}

func validateOptionalIdentityFilter(
	workspaceID string,
	appID int64,
	platformID int64,
	platformUserID string,
) error {
	if err := services.ValidateWorkspaceID(workspaceID); err != nil {
		return err
	}

	if appID < 0 || platformID < 0 ||
		(platformUserID != "" && (appID <= 0 || platformID <= 0)) {
		return repository.ErrPaymentReportInvalid
	}

	return nil
}
