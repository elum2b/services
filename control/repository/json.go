package repository

import (
	json "github.com/goccy/go-json"
	"github.com/sqlc-dev/pqtype"
)

func nullRawMessage(value pqtype.NullRawMessage) json.RawMessage {
	if !value.Valid {
		return nil
	}
	return json.RawMessage(value.RawMessage)
}

func rawMessageParam(value json.RawMessage) pqtype.NullRawMessage {
	if len(value) == 0 {
		return pqtype.NullRawMessage{}
	}
	return pqtype.NullRawMessage{RawMessage: value, Valid: true}
}
