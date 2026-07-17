package internalapi

import (
	"context"
	"time"

	"github.com/elum2b/services/tasks/repository"
)

type PartnerScriptModel struct {
	Provider  string    `json:"provider"`
	IsEnabled bool      `json:"is_enabled"`
	Version   string    `json:"version"`
	Source    string    `json:"source"`
	CreatedAt time.Time `json:"created_at,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

func (i *Internal) SavePartnerScript(ctx context.Context, params PartnerScriptModel) error {
	mergedCtx, cancel := i.withContext(ctx)
	defer cancel()

	return i.repository.SavePartnerScript(mergedCtx, repository.SavePartnerScriptParams{
		Provider:  params.Provider,
		IsEnabled: params.IsEnabled,
		Version:   params.Version,
		Source:    params.Source,
	})
}

func (i *Internal) GetPartnerScript(ctx context.Context, provider string) (PartnerScriptModel, bool, error) {
	mergedCtx, cancel := i.withContext(ctx)
	defer cancel()

	script, found, err := i.repository.GetPartnerScript(mergedCtx, provider)
	if err != nil || !found {
		return PartnerScriptModel{}, found, err
	}

	return mapPartnerScript(script), true, nil
}

func (i *Internal) ListPartnerScripts(ctx context.Context) ([]PartnerScriptModel, error) {
	mergedCtx, cancel := i.withContext(ctx)
	defer cancel()

	scripts, err := i.repository.ListPartnerScripts(mergedCtx)
	if err != nil {
		return nil, err
	}

	result := make([]PartnerScriptModel, 0, len(scripts))
	for _, script := range scripts {
		result = append(result, mapPartnerScript(script))
	}

	return result, nil
}

func mapPartnerScript(script repository.PartnerScript) PartnerScriptModel {
	return PartnerScriptModel{
		Provider:  script.Provider,
		IsEnabled: script.IsEnabled,
		Version:   script.Version,
		Source:    script.Source,
		CreatedAt: script.CreatedAt,
		UpdatedAt: script.UpdatedAt,
	}
}
