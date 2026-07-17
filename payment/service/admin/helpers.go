package admin

import "database/sql"

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

func nullString(value *string) sql.NullString {
	if value == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *value, Valid: true}
}
