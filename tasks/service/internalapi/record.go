package internalapi

import (
	"context"
	json "github.com/goccy/go-json"
	"time"

	"github.com/elum2b/services/tasks/repository"
)

type Identity = repository.Identity

type RecordParams struct {
	Identity         Identity
	ActionKey        string
	Amount           uint64
	Source           string
	ExternalEventKey string
	Payload          json.RawMessage
	Now              time.Time
}

type RecordResult = repository.RecordResult

func (i *Internal) Record(ctx context.Context, params RecordParams) (RecordResult, error) {
	mergedCtx, cancel := i.withContext(ctx)
	defer cancel()
	return i.repository.Record(mergedCtx, repository.RecordParams(params))
}
