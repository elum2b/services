package utils

func Ref[T any](value T) *T {
	return &value
}

func Deref[T any](value *T) T {
	var zero T
	if value != nil {
		return *value
	}
	return zero
}
