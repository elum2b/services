package admin

import "context"

type AddCodesParams struct {
	WorkspaceID string
	CPAID       string
	Codes       []string
}

func (a *Admin) AddCodes(ctx context.Context, params AddCodesParams) (int, error) {

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	return a.repository.AddCodes(mergedCtx, params.WorkspaceID, params.CPAID, params.Codes)

}

func (a *Admin) DeleteAvailableCodes(ctx context.Context, workspaceID, cpaID string) (int64, error) {

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	return a.repository.DeleteAvailableCodes(mergedCtx, workspaceID, cpaID)

}

func (a *Admin) DeleteIssuedCodes(ctx context.Context, workspaceID, cpaID string) (int64, error) {

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	return a.repository.DeleteIssuedCodes(mergedCtx, workspaceID, cpaID)

}

func (a *Admin) DeleteCompletedCodes(ctx context.Context, workspaceID, cpaID string) (int64, error) {

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	return a.repository.DeleteCompletedCodes(mergedCtx, workspaceID, cpaID)

}
