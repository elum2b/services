package repository

import (
	"database/sql"
	"fmt"
)

func allowed(value sql.NullBool) bool {

	return value.Valid && value.Bool

}

func position(value any) (int32, error) {

	switch value := value.(type) {
	case int64:
		return int32(value), nil
	case uint64:
		return int32(value), nil
	case int32:
		return value, nil
	case int:
		return int32(value), nil
	case []byte:
		var result int32
		if _, err := fmt.Sscan(string(value), &result); err != nil {
			return 0, err
		}
		return result, nil
	default:
		return 0, fmt.Errorf("unexpected control position type %T", value)
	}

}

func requireHigher(actorPosition, targetPosition int32) error {

	if actorPosition >= targetPosition {
		return ErrRoleHierarchy
	}

	return nil

}
